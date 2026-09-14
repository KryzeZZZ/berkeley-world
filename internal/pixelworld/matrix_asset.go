package pixelworld

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"SAO/internal/ai"
)

type MatrixAsset struct {
	ID      string            `json:"id"`
	Width   int               `json:"width"`
	Height  int               `json:"height"`
	Palette map[string]string `json:"palette"`
	Pixels  []string          `json:"pixels"`
}

const matrixAssetVersion = "v2"

func LoadOrGenerateAsset(kind string) (MatrixAsset, error) {
	type preset struct {
		subject       string
		width, height int
	}
	presets := map[string]preset{
		"staff":  {"a simple cyan crystal magic staff with dark wood and gold runes", 16, 16},
		"player": {"a deliberately simple pink-haired female RPG player character; no weapon", 16, 24},
		"npc":    {"a deliberately simple green-cloaked forest scout NPC", 16, 24},
	}
	entry, ok := presets[kind]
	if !ok {
		return MatrixAsset{}, fmt.Errorf("unsupported matrix asset %q", kind)
	}
	return loadOrGenerateMatrix(kind, entry.subject, entry.width, entry.height, kind+"-"+matrixAssetVersion)
}

func LoadOrGenerateTerrainAsset(kind string) (MatrixAsset, error) {
	subjects := map[string]string{
		"grass":        "a seamless forest grass ground tile with small natural texture",
		"path":         "a seamless worn ochre dirt path ground tile",
		"water":        "a seamless shallow teal forest stream water tile",
		"tree":         "a dense top-down forest tree canopy and trunk tile that blocks movement",
		"rubble":       "a broken grey stone and dirt ground tile",
		"rubble-grass": "a damaged forest grass ground tile with scattered debris",
		"rubble-path":  "a damaged ochre dirt path tile with scattered debris",
		"rubble-water": "a damaged shallow stream tile with scattered debris",
		"rubble-tree":  "a damaged forest tree and broken trunk ground tile",
	}
	subject, ok := subjects[kind]
	if !ok {
		return MatrixAsset{}, fmt.Errorf("unsupported terrain asset %q", kind)
	}
	return loadOrGenerateMatrix("terrain-"+kind, subject, 32, 32, "terrain-"+kind+"-"+matrixAssetVersion)
}

// LoadOrGenerateObjectAsset turns the complete local entity record into an
// object-specific sprite request. Two chests with different names or states
// are therefore different assets instead of sharing a generic kind icon.
func LoadOrGenerateObjectAsset(entity Entity) (MatrixAsset, error) {
	spec, err := json.Marshal(struct {
		ID       string         `json:"id"`
		Kind     string         `json:"kind"`
		Name     string         `json:"name"`
		Position Position       `json:"position"`
		State    map[string]any `json:"state"`
	}{entity.ID, entity.Kind, entity.Name, entity.Position, entity.State})
	if err != nil {
		return MatrixAsset{}, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(spec))[:12]
	subject := "a 32x32 top-down RPG world object defined by this authoritative world JSON: " + string(spec)
	width, height := 32, 32
	if entity.Kind == "npc" {
		width, height = 16, 24
		subject = "a deliberately simple NPC defined by this authoritative world JSON: " + string(spec)
	}
	return loadOrGenerateMatrix("object-"+entity.ID, subject, width, height, "object-"+entity.ID+"-"+digest)
}

func loadOrGenerateMatrix(id, subject string, width, height int, cacheKey string) (MatrixAsset, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		asset, err := loadOrGenerateMatrixAttempt(id, subject, width, height, cacheKey)
		if err == nil {
			return asset, nil
		}
		lastErr = err
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 400 * time.Millisecond)
		}
	}
	return MatrixAsset{}, fmt.Errorf("asset generation failed after retries: %w", lastErr)
}

func loadOrGenerateMatrixAttempt(id, subject string, width, height int, cacheKey string) (MatrixAsset, error) {
	// The first RLE format allowed malformed model output to be clipped during
	// rendering. Keep it out of the new cache by versioning the asset contract.
	path := filepath.Join("data", "pixel_assets", cacheKey+".json")
	if data, err := os.ReadFile(path); err == nil {
		var asset MatrixAsset
		if json.Unmarshal(data, &asset) == nil && validateMatrixAsset(asset, width, height) == nil {
			return asset, nil
		}
	}
	cfg, err := ai.LoadConfigFromFile("config/ai_referee.json")
	if err != nil {
		return MatrixAsset{}, err
	}
	prompt := fmt.Sprintf("Return JSON only. Create a crisp pixel-art sprite of %s. Fields: id,width,height,palette,pixels. width must be %d and height must be %d. palette keys must be one ASCII character. palette must include '.' with value 'transparent'. pixels must be an array of exactly %d strings; EVERY string must contain exactly %d ASCII palette characters, with no RLE, no spaces, and no tabs. Use 4-8 colors, a readable silhouette, dark outline, and transparent background. Do not add text or a shadow. No markdown or explanations.", subject, width, height, height, width)
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "temperature": 0.15, "messages": []map[string]string{{"role": "system", "content": "Output strict JSON only."}, {"role": "user", "content": prompt}}, "response_format": map[string]any{"type": "json_object"}})
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return MatrixAsset{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := (&http.Client{Timeout: cfg.Timeout}).Do(req)
	if err != nil {
		return MatrixAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MatrixAsset{}, fmt.Errorf("opus asset request status %d", resp.StatusCode)
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil || len(wrapper.Choices) == 0 {
		return MatrixAsset{}, fmt.Errorf("invalid opus asset response")
	}
	content := strings.TrimSpace(wrapper.Choices[0].Message.Content)
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return MatrixAsset{}, fmt.Errorf("opus asset response did not contain JSON")
	}
	var asset MatrixAsset
	if err := json.Unmarshal([]byte(content[start:end+1]), &asset); err != nil {
		return MatrixAsset{}, err
	}
	asset.ID = id
	normalizeMatrixRows(&asset, width, height)
	if err := validateMatrixAsset(asset, width, height); err != nil {
		return MatrixAsset{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return MatrixAsset{}, err
	}
	data, _ := json.MarshalIndent(asset, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return MatrixAsset{}, err
	}
	return asset, nil
}

// Models occasionally omit or add a trailing transparent cell. Because rows
// are independent fixed grids, repairing only that right edge cannot shift the
// sprite content as the old RLE format did.
func normalizeMatrixRows(asset *MatrixAsset, width, height int) {
	if len(asset.Pixels) != height {
		return
	}
	for index, row := range asset.Pixels {
		if len(row) < width {
			asset.Pixels[index] = row + strings.Repeat(".", width-len(row))
		} else if len(row) > width {
			asset.Pixels[index] = row[:width]
		}
	}
	for _, row := range asset.Pixels {
		for index := 0; index < len(row); index++ {
			symbol := string(row[index])
			if row[index] <= 127 {
				if _, defined := asset.Palette[symbol]; !defined {
					asset.Palette[symbol] = "transparent"
				}
			}
		}
	}
}

func RenderMatrixAsset(asset MatrixAsset) (image.Image, error) {
	if err := validateMatrixAsset(asset, asset.Width, asset.Height); err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, asset.Width, asset.Height))
	for y, row := range asset.Pixels {
		for x := 0; x < len(row); x++ {
			symbol := string(row[x])
			rgba, err := parseHex(asset.Palette[symbol])
			if err != nil {
				return nil, err
			}
			canvas.SetRGBA(x, y, rgba)
		}
	}
	return canvas, nil
}

func validateMatrixAsset(asset MatrixAsset, width, height int) error {
	if asset.Width != width || asset.Height != height || len(asset.Pixels) != height || len(asset.Palette) < 2 || len(asset.Palette) > 16 {
		return fmt.Errorf("asset dimensions do not match request")
	}
	if asset.Palette["."] != "transparent" {
		return fmt.Errorf("palette must define transparent '.'")
	}
	for _, row := range asset.Pixels {
		if len(row) != asset.Width {
			return fmt.Errorf("pixel row must be exactly %d characters", asset.Width)
		}
		for i := 0; i < len(row); i++ {
			if row[i] > 127 {
				return fmt.Errorf("pixel row contains non-ASCII symbol")
			}
			if _, ok := asset.Palette[string(row[i])]; !ok {
				return fmt.Errorf("undefined palette symbol")
			}
		}
	}
	return nil
}

func parseHex(raw string) (color.RGBA, error) {
	if strings.EqualFold(strings.TrimSpace(raw), "transparent") {
		return color.RGBA{}, nil
	}
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "#")
	if len(raw) == 6 {
		raw += "ff"
	}
	if len(raw) != 8 {
		return color.RGBA{}, fmt.Errorf("expected RRGGBBAA")
	}
	values := make([]uint8, 4)
	for i := 0; i < 4; i++ {
		v, e := strconv.ParseUint(raw[i*2:i*2+2], 16, 8)
		if e != nil {
			return color.RGBA{}, e
		}
		values[i] = uint8(v)
	}
	return color.RGBA{values[0], values[1], values[2], values[3]}, nil
}
