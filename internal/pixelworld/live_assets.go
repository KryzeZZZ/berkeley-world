package pixelworld

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
)

// LiveObjectAssets serves generated object assets without blocking gameplay.
// It is intentionally filesystem-backed: the pixel prototype does not use the
// database for generated visuals.
type LiveObjectAssets struct {
	mu     sync.Mutex
	jobs   map[string]bool
	notify func(LiveAssetEvent)
}

type LiveAssetEvent struct {
	EntityID string `json:"entity_id"`
	AssetKey string `json:"asset_key"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

func NewLiveObjectAssets(notify func(LiveAssetEvent)) *LiveObjectAssets {
	return &LiveObjectAssets{jobs: map[string]bool{}, notify: notify}
}

// Request returns a cached asset when ready. Otherwise it schedules one
// background generation job and lets the caller render an immediate fallback.
func (assets *LiveObjectAssets) Request(entity Entity) (image.Image, bool, error) {
	path, key, err := liveObjectAssetPath(entity)
	if err != nil {
		return nil, false, err
	}
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		decoded, decodeErr := png.Decode(file)
		if decodeErr == nil {
			return decoded, true, nil
		}
	}

	assets.mu.Lock()
	if !assets.jobs[key] {
		assets.jobs[key] = true
		go assets.generate(entity, path, key)
	}
	assets.mu.Unlock()
	return nil, false, nil
}

func (assets *LiveObjectAssets) generate(entity Entity, path, key string) {
	status := LiveAssetEvent{EntityID: entity.ID, AssetKey: key, Status: "ready"}
	defer func() {
		assets.mu.Lock()
		delete(assets.jobs, key)
		assets.mu.Unlock()
		if assets.notify != nil {
			assets.notify(status)
		}
	}()

	cfg, err := loadComfyConfig()
	if err == nil {
		generated, generationErr := generateComfyObject(cfg, entity, "")
		if generationErr != nil {
			err = generationErr
		} else {
			prepared := normalizeComfyObjectCandidate(generated)
			if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
				err = mkdirErr
			} else if file, createErr := os.Create(path); createErr != nil {
				err = createErr
			} else {
				err = png.Encode(file, prepared)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
	}
	if err != nil {
		status.Status = "failed"
		status.Detail = err.Error()
	}
}

func liveObjectAssetPath(entity Entity) (string, string, error) {
	cfg, err := loadComfyConfig()
	if err != nil {
		return "", "", err
	}
	spec, err := json.Marshal(struct {
		Entity Entity `json:"entity"`
		Model  string `json:"model"`
		LoRA   string `json:"lora"`
		Steps  int    `json:"steps"`
	}{Entity: entity, Model: cfg.ObjectUNet, LoRA: objectLoRAName(cfg, entity.Kind), Steps: cfg.ObjectSteps})
	if err != nil {
		return "", "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(spec))[:16]
	key := "live/object/" + entity.ID + "/" + digest
	return filepath.Join(pixelAssetRoot, "live", "objects", entity.ID+"-"+digest+".png"), key, nil
}

func objectLoRAName(cfg comfyConfig, kind string) string {
	lora, _ := objectLoRAForKind(cfg, kind)
	return lora
}
