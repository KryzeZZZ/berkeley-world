package pixelworld

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const pixelAssetRoot = "data/pixel_assets"
const pixelAssetManifestPath = pixelAssetRoot + "/asset-manifest.json"

// AssetManifest separates generated candidates from game-ready assets. Only
// records marked approved should be used by a runtime renderer.
type AssetManifest struct {
	Version int                    `json:"version"`
	Assets  map[string]AssetRecord `json:"assets"`
}

type AssetRecord struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Status     string `json:"status"`
	ApprovedAt string `json:"approved_at,omitempty"`
}

type AssetCandidate struct {
	Key       string          `json:"key"`
	Kind      string          `json:"kind"`
	File      string          `json:"file"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	Generator string          `json:"generator"`
	CreatedAt string          `json:"created_at"`
	Status    string          `json:"status"`
	Review    AssetReviewInfo `json:"review"`
}

// AssetReviewInfo is advisory only. A human must still explicitly approve an
// asset before it can be used by the runtime renderer.
type AssetReviewInfo struct {
	Status         string   `json:"status"`
	OpaqueCoverage float64  `json:"opaque_coverage"`
	DarkPixelRatio float64  `json:"dark_pixel_ratio"`
	Warnings       []string `json:"warnings,omitempty"`
}

func LoadAssetManifest() (AssetManifest, error) {
	data, err := os.ReadFile(pixelAssetManifestPath)
	if os.IsNotExist(err) {
		return AssetManifest{Version: 1, Assets: map[string]AssetRecord{}}, nil
	}
	if err != nil {
		return AssetManifest{}, err
	}
	var manifest AssetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return AssetManifest{}, fmt.Errorf("decode pixel asset manifest: %w", err)
	}
	if manifest.Version != 1 {
		return AssetManifest{}, fmt.Errorf("unsupported pixel asset manifest version %d", manifest.Version)
	}
	if manifest.Assets == nil {
		manifest.Assets = map[string]AssetRecord{}
	}
	return manifest, nil
}

func ListAssetRecords() ([]AssetRecord, error) {
	manifest, err := LoadAssetManifest()
	if err != nil {
		return nil, err
	}
	items := make([]AssetRecord, 0, len(manifest.Assets))
	for _, record := range manifest.Assets {
		items = append(items, record)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func ApproveAsset(key, kind, file string) (AssetRecord, error) {
	key = strings.TrimSpace(key)
	kind = strings.TrimSpace(kind)
	if key == "" || kind == "" {
		return AssetRecord{}, fmt.Errorf("asset key and kind are required")
	}
	relative, fullPath, err := assetFilePath(file)
	if err != nil {
		return AssetRecord{}, err
	}
	handle, err := os.Open(fullPath)
	if err != nil {
		return AssetRecord{}, err
	}
	defer handle.Close()
	decoded, err := png.Decode(handle)
	if err != nil {
		return AssetRecord{}, fmt.Errorf("approved asset must be a png: %w", err)
	}
	bounds := decoded.Bounds()
	record := AssetRecord{
		Key: key, Kind: kind, File: relative, Width: bounds.Dx(), Height: bounds.Dy(),
		Status: "approved", ApprovedAt: time.Now().UTC().Format(time.RFC3339),
	}
	manifest, err := LoadAssetManifest()
	if err != nil {
		return AssetRecord{}, err
	}
	manifest.Assets[key] = record
	if err := saveAssetManifest(manifest); err != nil {
		return AssetRecord{}, err
	}
	return record, nil
}

func LoadApprovedAsset(key string) (image.Image, bool, error) {
	manifest, err := LoadAssetManifest()
	if err != nil {
		return nil, false, err
	}
	record, found := manifest.Assets[key]
	if !found || record.Status != "approved" {
		return nil, false, nil
	}
	_, fullPath, err := assetFilePath(record.File)
	if err != nil {
		return nil, false, err
	}
	handle, err := os.Open(fullPath)
	if err != nil {
		return nil, false, err
	}
	defer handle.Close()
	decoded, err := png.Decode(handle)
	if err != nil {
		return nil, false, err
	}
	return decoded, true, nil
}

func saveAssetManifest(manifest AssetManifest) error {
	if err := os.MkdirAll(pixelAssetRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pixelAssetManifestPath, append(data, '\n'), 0o644)
}

func saveAssetCandidate(candidate AssetCandidate) error {
	_, fullPath, err := assetFilePath(candidate.File)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(strings.TrimSuffix(fullPath, filepath.Ext(fullPath))+".json", append(data, '\n'), 0o644)
}

func assetFilePath(file string) (string, string, error) {
	clean := filepath.Clean(strings.TrimSpace(file))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", "", fmt.Errorf("asset file must be relative to %s", pixelAssetRoot)
	}
	fullPath := filepath.Join(pixelAssetRoot, clean)
	return filepath.ToSlash(clean), fullPath, nil
}
