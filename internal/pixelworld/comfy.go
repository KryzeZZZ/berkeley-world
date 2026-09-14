package pixelworld

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type comfyConfig struct {
	BaseURL                  string             `json:"base_url"`
	Checkpoint               string             `json:"checkpoint"`
	ObjectUNet               string             `json:"object_unet"`
	ObjectTextEncoder        string             `json:"object_text_encoder"`
	ObjectVAE                string             `json:"object_vae"`
	ObjectSteps              int                `json:"object_steps"`
	ObjectWidth              int                `json:"object_width"`
	ObjectHeight             int                `json:"object_height"`
	ControlNet               string             `json:"controlnet"`
	LoRA                     string             `json:"lora"`
	LoRAStrength             float64            `json:"lora_strength"`
	ObjectLoRA               string             `json:"object_lora"`
	ObjectLoRAStrength       float64            `json:"object_lora_strength"`
	ObjectLoRAByKind         map[string]string  `json:"object_lora_by_kind"`
	ObjectLoRAStrengthByKind map[string]float64 `json:"object_lora_strength_by_kind"`
	ObjectSpritesheet        bool               `json:"object_spritesheet"`
	UseLoRAForMap            bool               `json:"use_lora_for_map"`
	UseLoRAForObjects        bool               `json:"use_lora_for_objects"`
	Timeout                  string             `json:"timeout"`
	InsecureSkipVerify       bool               `json:"insecure_skip_verify"`
}

func RenderComfyObject(entity Entity) (image.Image, error) {
	cfg, err := loadComfyConfig()
	if err != nil {
		return nil, err
	}
	spec, _ := json.Marshal(entity)
	objectLoRA, _ := objectLoRAForKind(cfg, entity.Kind)
	digest := fmt.Sprintf("%x", sha256.Sum256(append(spec, []byte(cfg.ObjectUNet+objectLoRA+fmt.Sprint(cfg.UseLoRAForObjects)+fmt.Sprint(cfg.ObjectSpritesheet)+"v8")...)))[:16]
	path := filepath.Join("data", "pixel_assets", "comfy-object-"+entity.ID+"-"+digest+".png")
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		if decoded, err := png.Decode(file); err == nil {
			return decoded, nil
		}
	}
	generated, err := generateComfyObject(cfg, entity, string(spec))
	if err != nil {
		return nil, err
	}
	width, height := comfyObjectSize(entity.Kind)
	sprite := pixelateObject(generated, width, height)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := png.Encode(file, sprite); err != nil {
		return nil, err
	}
	return sprite, nil
}

// GenerateComfyObjectCandidate writes a review-only asset. It deliberately
// bypasses the runtime cache so generation cannot alter a live client view.
func GenerateComfyObjectCandidate(entity Entity) (AssetCandidate, error) {
	cfg, err := loadComfyConfig()
	if err != nil {
		return AssetCandidate{}, err
	}
	spec, _ := json.Marshal(entity)
	generated, err := generateComfyObject(cfg, entity, string(spec))
	if err != nil {
		return AssetCandidate{}, err
	}
	// Keep review candidates at their generated resolution. Reducing a 1024px
	// asset to a game tile here was the source of the previous mosaic output.
	sprite := normalizeComfyObjectCandidate(generated)
	bounds := sprite.Bounds()
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	relative := filepath.ToSlash(filepath.Join("candidates", "objects", entity.ID+"-"+stamp+".png"))
	_, fullPath, err := assetFilePath(relative)
	if err != nil {
		return AssetCandidate{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return AssetCandidate{}, err
	}
	file, err := os.Create(fullPath)
	if err != nil {
		return AssetCandidate{}, err
	}
	if err := png.Encode(file, sprite); err != nil {
		_ = file.Close()
		return AssetCandidate{}, err
	}
	if err := file.Close(); err != nil {
		return AssetCandidate{}, err
	}
	candidate := AssetCandidate{
		Key: "object/" + entity.ID, Kind: "object", File: relative, Width: bounds.Dx(), Height: bounds.Dy(),
		Generator: "comfyui-flux2", CreatedAt: time.Now().UTC().Format(time.RFC3339), Status: "candidate",
		Review: assessObjectCandidate(sprite),
	}
	if err := saveAssetCandidate(candidate); err != nil {
		return AssetCandidate{}, err
	}
	return candidate, nil
}

func comfyObjectSize(kind string) (int, int) {
	if kind == "npc" {
		return 24, 36
	}
	if kind == "gate" {
		return 64, 64
	}
	return 64, 64
}

func RenderComfyMap(snapshot Snapshot) (image.Image, error) {
	cfg, err := loadComfyConfig()
	if err != nil {
		return nil, err
	}
	keyData, _ := json.Marshal(struct {
		SceneID    string `json:"scene_id"`
		Checkpoint string `json:"checkpoint"`
		ControlNet string `json:"controlnet"`
		LoRA       string `json:"lora"`
		UseLoRA    bool   `json:"use_lora"`
		PromptV    string `json:"prompt_version"`
	}{snapshot.SceneID, cfg.Checkpoint, cfg.ControlNet, cfg.LoRA, cfg.UseLoRAForMap, "v7"})
	digest := fmt.Sprintf("%x", sha256.Sum256(keyData))[:16]
	path := filepath.Join("data", "pixel_assets", "comfy-map-"+digest+".png")
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		if decoded, err := png.Decode(file); err == nil {
			return overlayDestroyedTiles(decoded, snapshot), nil
		}
	}

	generated, err := generateComfyMap(cfg, snapshot)
	if err != nil {
		return nil, err
	}
	pixelated := pixelateMap(generated, MapWidth*RenderTileSize, MapHeight*RenderTileSize)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := png.Encode(file, pixelated); err != nil {
		return nil, err
	}
	return overlayDestroyedTiles(pixelated, snapshot), nil
}

func loadComfyConfig() (comfyConfig, error) {
	data, err := os.ReadFile("config/comfyui.json")
	if err != nil {
		return comfyConfig{}, err
	}
	var cfg comfyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return comfyConfig{}, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Checkpoint) == "" || strings.TrimSpace(cfg.ControlNet) == "" {
		return comfyConfig{}, fmt.Errorf("comfyui base_url, checkpoint and controlnet are required")
	}
	if cfg.UseLoRAForMap && strings.TrimSpace(cfg.LoRA) == "" {
		return comfyConfig{}, fmt.Errorf("comfyui lora is required when enabled for maps")
	}
	if cfg.UseLoRAForMap && cfg.LoRAStrength == 0 {
		cfg.LoRAStrength = 0.85
	}
	if cfg.ObjectSteps == 0 {
		cfg.ObjectSteps = 4
	}
	if cfg.ObjectWidth == 0 {
		cfg.ObjectWidth = 1024
	}
	if cfg.ObjectHeight == 0 {
		cfg.ObjectHeight = cfg.ObjectWidth
	}
	if strings.TrimSpace(cfg.ObjectUNet) == "" || strings.TrimSpace(cfg.ObjectTextEncoder) == "" || strings.TrimSpace(cfg.ObjectVAE) == "" {
		return comfyConfig{}, fmt.Errorf("comfyui object_unet, object_text_encoder and object_vae are required for FLUX.2 object candidates")
	}
	return cfg, nil
}

func generateComfyMap(cfg comfyConfig, snapshot Snapshot) (image.Image, error) {
	timeout := 3 * time.Minute
	if cfg.Timeout != "" {
		if parsed, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsed
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 -- explicit development config.
	client := &http.Client{Timeout: timeout, Transport: transport}
	clientID := fmt.Sprintf("sao-map-%d", time.Now().UnixNano())
	layoutName, err := uploadComfyLayout(client, cfg.BaseURL, snapshot)
	if err != nil {
		return nil, err
	}
	positive := "full-frame edge-to-edge orthographic top-down 2D fantasy RPG battlemap of a forest ruin. Follow the supplied scribble layout: broad dark marks are a shallow stream and narrow dark marks are connected dirt paths. Fill the whole canvas with continuous playable forest floor, natural terrain, and a modest ruined clearing. No characters, creatures, items, text, UI, tile grid, islands, bridges, or empty space."
	if cfg.UseLoRAForMap {
		positive = "mapchip, 3232, " + positive
	}
	negative := "black background, black void, empty space, vignette, border frame, isolated island, floating land, bridge, characters, creatures, items, text, letters, numbers, user interface, panels, tiled pattern, repeated icons, seams, blurry, photorealistic"
	modelRef, clipRef := []any{"4", 0}, []any{"4", 1}
	workflow := map[string]any{
		"3":  map[string]any{"class_type": "KSampler", "inputs": map[string]any{"seed": time.Now().UnixNano(), "steps": 32, "cfg": 5.5, "sampler_name": "euler", "scheduler": "normal", "denoise": 1.0, "model": modelRef, "positive": []any{"11", 0}, "negative": []any{"7", 0}, "latent_image": []any{"5", 0}}},
		"4":  map[string]any{"class_type": "CheckpointLoaderSimple", "inputs": map[string]any{"ckpt_name": cfg.Checkpoint}},
		"5":  map[string]any{"class_type": "EmptyLatentImage", "inputs": map[string]any{"width": 960, "height": 672, "batch_size": 1}},
		"6":  map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"text": positive, "clip": clipRef}},
		"7":  map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"text": negative, "clip": clipRef}},
		"8":  map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"3", 0}, "vae": []any{"4", 2}}},
		"9":  map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"filename_prefix": "sao-map", "images": []any{"8", 0}}},
		"10": map[string]any{"class_type": "ControlNetLoader", "inputs": map[string]any{"control_net_name": cfg.ControlNet}},
		"11": map[string]any{"class_type": "ControlNetApply", "inputs": map[string]any{"conditioning": []any{"6", 0}, "control_net": []any{"10", 0}, "image": []any{"12", 0}, "strength": 0.65}},
		"12": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": layoutName}},
	}
	if cfg.UseLoRAForMap {
		workflow["13"] = map[string]any{"class_type": "LoraLoader", "inputs": map[string]any{"model": []any{"4", 0}, "clip": []any{"4", 1}, "lora_name": cfg.LoRA, "strength_model": cfg.LoRAStrength, "strength_clip": cfg.LoRAStrength}}
		workflow["3"].(map[string]any)["inputs"].(map[string]any)["model"] = []any{"13", 0}
		workflow["6"].(map[string]any)["inputs"].(map[string]any)["clip"] = []any{"13", 1}
		workflow["7"].(map[string]any)["inputs"].(map[string]any)["clip"] = []any{"13", 1}
	}
	body, _ := json.Marshal(map[string]any{"client_id": clientID, "prompt": workflow})
	response, err := client.Post(strings.TrimRight(cfg.BaseURL, "/")+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("comfy prompt status %d", response.StatusCode)
	}
	var queued struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&queued); err != nil || queued.PromptID == "" {
		return nil, fmt.Errorf("invalid comfy prompt response")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		imageURL, done, err := comfyResultURL(client, cfg.BaseURL, queued.PromptID)
		if err != nil {
			return nil, err
		}
		if done && imageURL != "" {
			result, err := client.Get(imageURL)
			if err != nil {
				return nil, err
			}
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("comfy image status %d", result.StatusCode)
			}
			return png.Decode(result.Body)
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("comfy map generation timed out")
}

func generateComfyObject(cfg comfyConfig, entity Entity, _ string) (image.Image, error) {
	timeout := 3 * time.Minute
	if cfg.Timeout != "" {
		if parsed, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsed
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 -- explicit development config.
	client := &http.Client{Timeout: timeout, Transport: transport}
	positive := comfyObjectPrompt(entity)
	if cfg.ObjectSpritesheet {
		positive += " Create a clean four by four sprite sheet containing sixteen separate variations of this same object, one object per cell, with generous empty background around every cell."
	} else {
		positive += " Create exactly one centered object that fills the central 70 percent of the image."
	}
	modelRef := []any{"1", 0}
	workflow := map[string]any{
		"1":  map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": cfg.ObjectUNet, "weight_dtype": "default"}},
		"2":  map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": cfg.ObjectTextEncoder, "type": "flux2", "device": "default"}},
		"3":  map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": cfg.ObjectVAE}},
		"4":  map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"text": positive, "clip": []any{"2", 0}}},
		"5":  map[string]any{"class_type": "ConditioningZeroOut", "inputs": map[string]any{"conditioning": []any{"4", 0}}},
		"6":  map[string]any{"class_type": "CFGGuider", "inputs": map[string]any{"model": modelRef, "positive": []any{"4", 0}, "negative": []any{"5", 0}, "cfg": 1.0}},
		"7":  map[string]any{"class_type": "KSamplerSelect", "inputs": map[string]any{"sampler_name": "euler"}},
		"8":  map[string]any{"class_type": "RandomNoise", "inputs": map[string]any{"noise_seed": time.Now().UnixNano()}},
		"9":  map[string]any{"class_type": "Flux2Scheduler", "inputs": map[string]any{"steps": cfg.ObjectSteps, "width": cfg.ObjectWidth, "height": cfg.ObjectHeight}},
		"10": map[string]any{"class_type": "EmptyFlux2LatentImage", "inputs": map[string]any{"width": cfg.ObjectWidth, "height": cfg.ObjectHeight, "batch_size": 1}},
		"11": map[string]any{"class_type": "SamplerCustomAdvanced", "inputs": map[string]any{"noise": []any{"8", 0}, "guider": []any{"6", 0}, "sampler": []any{"7", 0}, "sigmas": []any{"9", 0}, "latent_image": []any{"10", 0}}},
		"12": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"11", 0}, "vae": []any{"3", 0}}},
		"13": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"filename_prefix": "sao-flux2-object-" + entity.ID, "images": []any{"12", 0}}},
	}
	if cfg.UseLoRAForObjects {
		objectLoRA, strength := objectLoRAForKind(cfg, entity.Kind)
		workflow["14"] = map[string]any{"class_type": "LoraLoaderModelOnly", "inputs": map[string]any{"model": []any{"1", 0}, "lora_name": objectLoRA, "strength_model": strength}}
		workflow["6"].(map[string]any)["inputs"].(map[string]any)["model"] = []any{"14", 0}
	}
	body, _ := json.Marshal(map[string]any{"client_id": fmt.Sprintf("sao-object-%d", time.Now().UnixNano()), "prompt": workflow})
	response, err := client.Post(strings.TrimRight(cfg.BaseURL, "/")+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("comfy object prompt status %d", response.StatusCode)
	}
	var queued struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&queued); err != nil || queued.PromptID == "" {
		return nil, fmt.Errorf("invalid comfy object prompt response")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		imageURL, done, err := comfyResultURL(client, cfg.BaseURL, queued.PromptID)
		if err != nil {
			return nil, err
		}
		if done && imageURL != "" {
			result, err := client.Get(imageURL)
			if err != nil {
				return nil, err
			}
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("comfy object image status %d", result.StatusCode)
			}
			return png.Decode(result.Body)
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("comfy object generation timed out")
}

func comfyObjectPrompt(entity Entity) string {
	if entity.Kind == "npc" {
		return "single full body 2D RPG forest scout character sprite, front facing standing idle pose, green ranger cloak, brown boots, leather satchel, clean readable silhouette, centered, isolated on plain light background, no text, no border, no scenery"
	}
	base := "top-down orthographic 2D medieval RPG game asset, centered, clear readable silhouette, detailed hand-painted game sprite, isolated on a plain light background, no text, no border, no user interface, no scenery"
	subject := entity.Kind
	switch entity.Kind {
	case "chest":
		subject = "one ornate amber supply chest, closed wooden lid with brass trim"
	case "gate":
		subject = "one ruined stone gate with ancient carved arch"
	case "npc":
		subject = "one forest scout character, full body, standing idle, facing forward"
	case "crystal":
		subject = "one glowing aqua teleport crystal on a small stone plinth"
	}
	return "<tdp> " + base + ", " + subject
}

func objectLoRAForKind(cfg comfyConfig, kind string) (string, float64) {
	lora := strings.TrimSpace(cfg.ObjectLoRAByKind[kind])
	if lora == "" {
		lora = strings.TrimSpace(cfg.ObjectLoRA)
	}
	if lora == "" {
		lora = strings.TrimSpace(cfg.LoRA)
	}
	strength, found := cfg.ObjectLoRAStrengthByKind[kind]
	if !found || strength == 0 {
		strength = cfg.ObjectLoRAStrength
	}
	if strength == 0 {
		strength = cfg.LoRAStrength
	}
	return lora, strength
}

// normalizeComfyObjectCandidate removes only the connected plain background
// around an object. It deliberately keeps the generated object at full detail
// instead of reducing it to a low-resolution tile.
func normalizeComfyObjectCandidate(source image.Image) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return source
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.SetRGBA(x, y, color.RGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.RGBA))
		}
	}

	background := averageCornerColor(canvas)
	visited := make([]bool, width*height)
	queue := make([]image.Point, 0, width*2+height*2)
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= width || y >= height {
			return
		}
		index := y*width + x
		if visited[index] || !similarBackground(canvas.RGBAAt(x, y), background) {
			return
		}
		visited[index] = true
		queue = append(queue, image.Pt(x, y))
	}
	for x := 0; x < width; x++ {
		push(x, 0)
		push(x, height-1)
	}
	for y := 1; y < height-1; y++ {
		push(0, y)
		push(width-1, y)
	}
	for head := 0; head < len(queue); head++ {
		point := queue[head]
		pixel := canvas.RGBAAt(point.X, point.Y)
		pixel.A = 0
		canvas.SetRGBA(point.X, point.Y, pixel)
		push(point.X+1, point.Y)
		push(point.X-1, point.Y)
		push(point.X, point.Y+1)
		push(point.X, point.Y-1)
	}

	content := transparentContentBounds(canvas)
	if content.Empty() {
		return source
	}
	padding := maxInt(16, maxInt(content.Dx(), content.Dy())/16)
	content = content.Inset(-padding).Intersect(canvas.Bounds())
	cropped := image.NewRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	for y := content.Min.Y; y < content.Max.Y; y++ {
		for x := content.Min.X; x < content.Max.X; x++ {
			cropped.SetRGBA(x-content.Min.X, y-content.Min.Y, canvas.RGBAAt(x, y))
		}
	}
	return cropped
}

func assessObjectCandidate(source image.Image) AssetReviewInfo {
	bounds := source.Bounds()
	total := bounds.Dx() * bounds.Dy()
	if total == 0 {
		return AssetReviewInfo{Status: "manual_review", Warnings: []string{"empty image"}}
	}
	opaque, dark := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.RGBAModel.Convert(source.At(x, y)).(color.RGBA)
			if pixel.A == 0 {
				continue
			}
			opaque++
			luminance := (int(pixel.R)*212 + int(pixel.G)*715 + int(pixel.B)*72) / 1000
			if luminance < 35 {
				dark++
			}
		}
	}
	review := AssetReviewInfo{
		Status:         "manual_review",
		OpaqueCoverage: float64(opaque) / float64(total),
	}
	if opaque > 0 {
		review.DarkPixelRatio = float64(dark) / float64(opaque)
	}
	if review.OpaqueCoverage > 0.90 {
		review.Warnings = append(review.Warnings, "subject occupies nearly the whole canvas")
	}
	if review.DarkPixelRatio > 0.55 {
		review.Warnings = append(review.Warnings, "asset is predominantly dark; inspect silhouette and detail")
	}
	return review
}

func averageCornerColor(canvas *image.RGBA) color.RGBA {
	points := []image.Point{{0, 0}, {canvas.Bounds().Dx() - 1, 0}, {0, canvas.Bounds().Dy() - 1}, {canvas.Bounds().Dx() - 1, canvas.Bounds().Dy() - 1}}
	var red, green, blue, alpha uint32
	for _, point := range points {
		pixel := canvas.RGBAAt(point.X, point.Y)
		red += uint32(pixel.R)
		green += uint32(pixel.G)
		blue += uint32(pixel.B)
		alpha += uint32(pixel.A)
	}
	return color.RGBA{R: uint8(red / uint32(len(points))), G: uint8(green / uint32(len(points))), B: uint8(blue / uint32(len(points))), A: uint8(alpha / uint32(len(points)))}
}

func similarBackground(pixel, background color.RGBA) bool {
	return pixel.A > 0 && absInt(int(pixel.R)-int(background.R)) <= 24 && absInt(int(pixel.G)-int(background.G)) <= 24 && absInt(int(pixel.B)-int(background.B)) <= 24
}

func transparentContentBounds(canvas *image.RGBA) image.Rectangle {
	bounds := canvas.Bounds()
	minX, minY, maxX, maxY := bounds.Max.X, bounds.Max.Y, bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canvas.RGBAAt(x, y).A == 0 {
				continue
			}
			minX, minY = minInt(minX, x), minInt(minY, y)
			maxX, maxY = maxInt(maxX, x+1), maxInt(maxY, y+1)
		}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func comfyResultURL(client *http.Client, baseURL, promptID string) (string, bool, error) {
	response, err := client.Get(strings.TrimRight(baseURL, "/") + "/history/" + url.PathEscape(promptID))
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("comfy history status %d", response.StatusCode)
	}
	var history map[string]struct {
		Outputs map[string]struct {
			Images []struct{ Filename, Subfolder, Type string }
		} `json:"outputs"`
		Status struct {
			Completed bool `json:"completed"`
		} `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		return "", false, err
	}
	record, ok := history[promptID]
	if !ok || !record.Status.Completed {
		return "", false, nil
	}
	for _, output := range record.Outputs {
		if len(output.Images) == 0 {
			continue
		}
		item := output.Images[0]
		query := url.Values{"filename": {item.Filename}, "subfolder": {item.Subfolder}, "type": {item.Type}}
		return strings.TrimRight(baseURL, "/") + "/view?" + query.Encode(), true, nil
	}
	return "", true, fmt.Errorf("comfy task completed without an image")
}

func uploadComfyLayout(client *http.Client, baseURL string, snapshot Snapshot) (string, error) {
	guide := renderMapLayoutGuide(snapshot)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, guide); err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("image", "sao-layout.png")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(imageData.Bytes()); err != nil {
		return "", err
	}
	_ = writer.WriteField("overwrite", "true")
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/upload/image", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("comfy layout upload status %d", response.StatusCode)
	}
	var uploaded struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&uploaded); err != nil || uploaded.Name == "" {
		return "", fmt.Errorf("invalid comfy layout upload response")
	}
	return uploaded.Name, nil
}

func renderMapLayoutGuide(snapshot Snapshot) image.Image {
	canvas := image.NewRGBA(image.Rect(0, 0, MapWidth*RenderTileSize, MapHeight*RenderTileSize))
	fill(canvas, 0, 0, canvas.Bounds().Dx(), canvas.Bounds().Dy(), color.RGBA{255, 255, 255, 255})
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			kind := tileKind(Position{X: x, Y: y}, snapshot)
			ink := color.RGBA{}
			radius := 0
			switch kind {
			case "water", "rubble-water":
				ink = color.RGBA{0, 0, 0, 255}
				radius = 15
			case "path", "rubble-path":
				ink = color.RGBA{90, 90, 90, 255}
				radius = 8
			default:
				continue
			}
			cx, cy := x*RenderTileSize+RenderTileSize/2, y*RenderTileSize+RenderTileSize/2
			drawGuideDisk(canvas, cx, cy, radius, ink)
			for _, direction := range []Position{{X: 1}, {Y: 1}} {
				neighbor := Position{X: x + direction.X, Y: y + direction.Y}
				if neighbor.X >= MapWidth || neighbor.Y >= MapHeight || tileKind(neighbor, snapshot) != kind {
					continue
				}
				drawGuideStroke(canvas, cx, cy, neighbor.X*RenderTileSize+RenderTileSize/2, neighbor.Y*RenderTileSize+RenderTileSize/2, radius, ink)
			}
		}
	}
	return canvas
}

func drawGuideStroke(canvas *image.RGBA, x0, y0, x1, y1, radius int, ink color.RGBA) {
	steps := max(abs(x1-x0), abs(y1-y0))
	for step := 0; step <= steps; step++ {
		x := x0 + (x1-x0)*step/steps
		y := y0 + (y1-y0)*step/steps
		drawGuideDisk(canvas, x, y, radius, ink)
	}
}

func drawGuideDisk(canvas *image.RGBA, centerX, centerY, radius int, ink color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				pixel(canvas, centerX+x, centerY+y, ink)
			}
		}
	}
}

func pixelateMap(source image.Image, width, height int) image.Image {
	lowWidth, lowHeight := width/2, height/2
	low := image.NewRGBA(image.Rect(0, 0, lowWidth, lowHeight))
	bounds := source.Bounds()
	for y := 0; y < lowHeight; y++ {
		for x := 0; x < lowWidth; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/lowWidth
			sy := bounds.Min.Y + y*bounds.Dy()/lowHeight
			c := color.RGBAModel.Convert(source.At(sx, sy)).(color.RGBA)
			c.R, c.G, c.B = c.R&0xf8, c.G&0xf8, c.B&0xf8
			low.SetRGBA(x, y, c)
		}
	}
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			output.Set(x, y, low.At(x/2, y/2))
		}
	}
	return output
}

func pixelateObject(source image.Image, width, height int) image.Image {
	source = extractPrimaryObject(source)
	bounds := source.Bounds()
	background := averageCorners(source)
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			sy := bounds.Min.Y + y*bounds.Dy()/height
			c := color.RGBAModel.Convert(source.At(sx, sy)).(color.RGBA)
			c.R, c.G, c.B = c.R&0xf8, c.G&0xf8, c.B&0xf8
			canvas.SetRGBA(x, y, c)
		}
	}
	// Only discard background connected to an edge. A global color-key erases dark
	// details from props whenever the generator uses a similarly dark backdrop.
	transparent := make([]bool, width*height)
	queue := make([]image.Point, 0, 2*(width+height))
	add := func(x, y int) {
		if x < 0 || x >= width || y < 0 || y >= height {
			return
		}
		index := y*width + x
		if transparent[index] || colorDistance(color.RGBAModel.Convert(canvas.At(x, y)).(color.RGBA), background) >= 64 {
			return
		}
		transparent[index] = true
		queue = append(queue, image.Pt(x, y))
	}
	for x := 0; x < width; x++ {
		add(x, 0)
		add(x, height-1)
	}
	for y := 1; y < height-1; y++ {
		add(0, y)
		add(width-1, y)
	}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		add(point.X+1, point.Y)
		add(point.X-1, point.Y)
		add(point.X, point.Y+1)
		add(point.X, point.Y-1)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if transparent[y*width+x] {
				canvas.SetRGBA(x, y, color.RGBA{})
			}
		}
	}
	return canvas
}

func extractPrimaryObject(source image.Image) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	background := averageCorners(source)
	isBackground := func(x, y int) bool {
		return colorDistance(color.RGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.RGBA), background) < 64
	}
	backgroundMask := make([]bool, width*height)
	queue := make([]image.Point, 0, 2*(width+height))
	addBackground := func(x, y int) {
		if x < 0 || x >= width || y < 0 || y >= height {
			return
		}
		index := y*width + x
		if backgroundMask[index] || !isBackground(x, y) {
			return
		}
		backgroundMask[index] = true
		queue = append(queue, image.Pt(x, y))
	}
	for x := 0; x < width; x++ {
		addBackground(x, 0)
		addBackground(x, height-1)
	}
	for y := 1; y < height-1; y++ {
		addBackground(0, y)
		addBackground(width-1, y)
	}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		addBackground(point.X+1, point.Y)
		addBackground(point.X-1, point.Y)
		addBackground(point.X, point.Y+1)
		addBackground(point.X, point.Y-1)
	}
	seen := make([]bool, width*height)
	best := image.Rectangle{}
	bestScore := 1e9
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			if backgroundMask[index] || seen[index] {
				continue
			}
			seen[index] = true
			component := []image.Point{image.Pt(x, y)}
			minX, minY, maxX, maxY := x, y, x, y
			for len(component) > 0 {
				point := component[0]
				component = component[1:]
				if point.X < minX {
					minX = point.X
				}
				if point.X > maxX {
					maxX = point.X
				}
				if point.Y < minY {
					minY = point.Y
				}
				if point.Y > maxY {
					maxY = point.Y
				}
				for _, next := range []image.Point{{point.X + 1, point.Y}, {point.X - 1, point.Y}, {point.X, point.Y + 1}, {point.X, point.Y - 1}} {
					if next.X < 0 || next.X >= width || next.Y < 0 || next.Y >= height {
						continue
					}
					nextIndex := next.Y*width + next.X
					if seen[nextIndex] || backgroundMask[nextIndex] {
						continue
					}
					seen[nextIndex] = true
					component = append(component, next)
				}
			}
			if len(component) < 24 {
				continue
			}
			box := image.Rect(minX, minY, maxX+1, maxY+1)
			centerDistance := float64(absInt(box.Min.X+box.Max.X-width)+absInt(box.Min.Y+box.Max.Y-height)) / float64(width+height)
			areaBonus := float64(box.Dx()*box.Dy()) / float64(width*height)
			score := centerDistance - areaBonus*0.15
			if score < bestScore {
				best, bestScore = box, score
			}
		}
	}
	if best.Empty() {
		return source
	}
	padding := maxInt(4, maxInt(best.Dx(), best.Dy())/8)
	best = image.Rect(maxInt(0, best.Min.X-padding), maxInt(0, best.Min.Y-padding), minInt(width, best.Max.X+padding), minInt(height, best.Max.Y+padding))
	return cropImage(source, best.Add(bounds.Min))
}

func cropImage(source image.Image, bounds image.Rectangle) image.Image {
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			canvas.Set(x-bounds.Min.X, y-bounds.Min.Y, source.At(x, y))
		}
	}
	return canvas
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func averageCorners(source image.Image) color.RGBA {
	bounds := source.Bounds()
	points := []image.Point{{bounds.Min.X, bounds.Min.Y}, {bounds.Max.X - 1, bounds.Min.Y}, {bounds.Min.X, bounds.Max.Y - 1}, {bounds.Max.X - 1, bounds.Max.Y - 1}}
	var red, green, blue uint32
	for _, point := range points {
		c := color.RGBAModel.Convert(source.At(point.X, point.Y)).(color.RGBA)
		red += uint32(c.R)
		green += uint32(c.G)
		blue += uint32(c.B)
	}
	return color.RGBA{R: uint8(red / 4), G: uint8(green / 4), B: uint8(blue / 4), A: 255}
}

func colorDistance(left, right color.RGBA) uint8 {
	delta := func(a, b uint8) uint8 {
		if a > b {
			return a - b
		}
		return b - a
	}
	return delta(left.R, right.R)/3 + delta(left.G, right.G)/3 + delta(left.B, right.B)/3
}

func overlayDestroyedTiles(source image.Image, snapshot Snapshot) image.Image {
	bounds := source.Bounds()
	canvas := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			canvas.Set(x, y, source.At(x, y))
		}
	}
	for key := range snapshot.ChangedTiles {
		var position Position
		if _, err := fmt.Sscanf(key, "%d,%d", &position.X, &position.Y); err != nil {
			continue
		}
		for y := 0; y < RenderTileSize; y++ {
			for x := 0; x < RenderTileSize; x++ {
				px, py := position.X*RenderTileSize+x, position.Y*RenderTileSize+y
				if !image.Pt(px, py).In(bounds) {
					continue
				}
				c := color.RGBAModel.Convert(canvas.At(px, py)).(color.RGBA)
				if (x*5+y*7+position.X+position.Y)%9 < 3 {
					c.R, c.G, c.B = c.R/3, c.G/3, c.B/3
				} else {
					c.R, c.G, c.B = c.R/2, c.G/2, c.B/2
				}
				canvas.SetRGBA(px, py, c)
			}
		}
	}
	return canvas
}

func RenderComfyTerrainAnimation(snapshot Snapshot, position Position) (image.Image, error) {
	scene, err := RenderComfyMap(snapshot)
	if err != nil {
		return nil, err
	}
	tile := image.NewRGBA(image.Rect(0, 0, RenderTileSize, RenderTileSize))
	for y := 0; y < RenderTileSize; y++ {
		for x := 0; x < RenderTileSize; x++ {
			tile.Set(x, y, scene.At(position.X*RenderTileSize+x, position.Y*RenderTileSize+y))
		}
	}
	return RenderRasterAnimation(tile, "break"), nil
}
