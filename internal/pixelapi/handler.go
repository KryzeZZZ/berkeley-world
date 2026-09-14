package pixelapi

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"SAO/internal/pixelworld"
)

type PixelMoveRequest struct {
	PlayerID string              `json:"player_id"`
	To       pixelworld.Position `json:"to"`
}

type PixelInteractRequest struct {
	PlayerID string `json:"player_id"`
	EntityID string `json:"entity_id"`
}

type PixelUseItemRequest struct {
	PlayerID string              `json:"player_id"`
	ItemID   string              `json:"item_id"`
	Target   pixelworld.Position `json:"target"`
}

// PixelHandler is deliberately separate from Handler so the text TRPG server
// does not initialize pixel state, rendering, or asset generation.
type PixelHandler struct {
	pixel  *pixelworld.Manager
	assets *pixelworld.LiveObjectAssets
}

func NewPixelHandler(worldPath string) (*PixelHandler, error) {
	pixel, err := pixelworld.NewManager(worldPath)
	if err != nil {
		return nil, fmt.Errorf("init local pixel world: %w", err)
	}
	handler := &PixelHandler{pixel: pixel}
	handler.assets = pixelworld.NewLiveObjectAssets(func(asset pixelworld.LiveAssetEvent) {
		handler.pixel.PublishRuntimeEvent(pixelworld.Event{
			Type: "asset_" + asset.Status, Title: "asset " + asset.Status, Detail: asset.EntityID + " " + asset.AssetKey,
		})
	})
	return handler, nil
}

func (h *PixelHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/pixel/world", h.handlePixelWorld)
	mux.HandleFunc("/pixel/move", h.handlePixelMove)
	mux.HandleFunc("/pixel/interact", h.handlePixelInteract)
	mux.HandleFunc("/pixel/use-item", h.handlePixelUseItem)
	mux.HandleFunc("/pixel/reset", h.handlePixelReset)
	mux.HandleFunc("/pixel/events", h.handlePixelEvents)
	mux.HandleFunc("/pixel/render/map.png", h.handlePixelMapPNG)
	mux.HandleFunc("/pixel/render/object/", h.handlePixelObjectPNG)
	mux.HandleFunc("/pixel/render/object-animation/", h.handlePixelObjectAnimationPNG)
	mux.HandleFunc("/pixel/render/terrain-animation/", h.handlePixelTerrainAnimationPNG)
	mux.HandleFunc("/pixel/render/sprite/", h.handlePixelSpritePNG)
	mux.HandleFunc("/pixel/render/animation/", h.handlePixelAnimationPNG)
}

func (h *PixelHandler) handlePixelAnimationPNG(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pixel/render/animation/"), ".png")
	valid := map[string]bool{"chest": true, "gate": true, "npc": true, "crystal": true}
	if r.Method != http.MethodGet || (!valid[key] && !strings.HasPrefix(key, "effect-break-")) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "animation not found"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, pixelworld.RenderAnimation(key))
}

func (h *PixelHandler) handlePixelMapPNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	image, err := pixelworld.RenderComfyMap(h.pixel.Snapshot())
	if err != nil {
		w.Header().Set("X-Pixel-Asset-Error", err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "map asset generation failed"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Pixel-Asset-Source", "comfyui-scene")
	_ = png.Encode(w, image)
}

func (h *PixelHandler) handlePixelObjectAnimationPNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pixel/render/object-animation/"), ".png")
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if action == "" {
		action = "interact"
	}
	for _, entity := range h.pixel.Snapshot().Entities {
		if entity.ID != id {
			continue
		}
		if action == "break" {
			animation, err := pixelworld.RenderComfyTerrainAnimation(h.pixel.Snapshot(), entity.Position)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "object destruction animation render failed"})
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("X-Pixel-Asset-Source", "comfyui-object-destruction")
			_ = png.Encode(w, animation)
			return
		}
		image, err := pixelworld.RenderComfyObject(entity)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "object animation generation failed"})
			return
		}
		animation := pixelworld.RenderRasterAnimation(image, action)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Pixel-Asset-Source", "comfyui-object-animation")
		_ = png.Encode(w, animation)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "object not found"})
}

func (h *PixelHandler) handlePixelTerrainAnimationPNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	x, xErr := strconv.Atoi(r.URL.Query().Get("x"))
	y, yErr := strconv.Atoi(r.URL.Query().Get("y"))
	if xErr != nil || yErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "x and y are required"})
		return
	}
	animation, err := pixelworld.RenderComfyTerrainAnimation(h.pixel.Snapshot(), pixelworld.Position{X: x, Y: y})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "terrain animation render failed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Pixel-Asset-Source", "comfyui-terrain-animation")
	_ = png.Encode(w, animation)
}

func (h *PixelHandler) handlePixelObjectPNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pixel/render/object/"), ".png")
	for _, entity := range h.pixel.Snapshot().Entities {
		if entity.ID != id {
			continue
		}
		if entity.State["destroyed"] == true {
			writeJSON(w, http.StatusGone, map[string]any{"error": "object destroyed"})
			return
		}
		image, ready, err := h.assets.Request(entity)
		if err != nil {
			w.Header().Set("X-Pixel-Asset-Error", err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "object asset generation failed", "detail": err.Error()})
			return
		}
		statusCode := http.StatusOK
		if !ready {
			image = pixelworld.RenderSprite(entity.Kind, entity.State)
			w.Header().Set("X-Pixel-Asset-Status", "generating")
			statusCode = http.StatusAccepted
		} else {
			w.Header().Set("X-Pixel-Asset-Status", "ready")
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Pixel-Asset-Source", "comfyui-live-object")
		w.WriteHeader(statusCode)
		_ = png.Encode(w, image)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "object not found"})
}

func (h *PixelHandler) handlePixelSpritePNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pixel/render/sprite/"), ".png")
	valid := map[string]bool{"player": true, "staff": true, "chest": true, "gate": true, "npc": true, "crystal": true}
	if !valid[key] {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "sprite not found"})
		return
	}
	state := map[string]any{}
	for _, entity := range h.pixel.Snapshot().Entities {
		if entity.Kind == key {
			state = entity.State
			break
		}
	}
	variant := strings.TrimSpace(r.URL.Query().Get("variant"))
	if variant == "open" && (key == "chest" || key == "gate") {
		state["opened"] = true
	}
	if variant == "closed" && (key == "chest" || key == "gate") {
		state["opened"] = false
	}
	if variant == "active" && key == "crystal" {
		state["active"] = true
	}
	if variant == "inactive" && key == "crystal" {
		state["active"] = false
	}
	w.Header().Set("Content-Type", "image/png")
	if key == "staff" || key == "player" || key == "npc" {
		if asset, err := pixelworld.LoadOrGenerateAsset(key); err == nil {
			if image, err := pixelworld.RenderMatrixAsset(asset); err == nil {
				w.Header().Set("X-Pixel-Asset-Source", "opus-matrix")
				_ = png.Encode(w, image)
				return
			}
		} else {
			w.Header().Set("X-Pixel-Asset-Error", err.Error())
		}
	}
	w.Header().Set("X-Pixel-Asset-Source", "procedural-fallback")
	_ = png.Encode(w, pixelworld.RenderSprite(key, state))
}

func (h *PixelHandler) handlePixelWorld(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "world": h.pixel.Snapshot()})
}

func (h *PixelHandler) handlePixelMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req PixelMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	snapshot, event, err := h.pixel.Move(strings.TrimSpace(req.PlayerID), req.To)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "world": snapshot, "event": event})
}

func (h *PixelHandler) handlePixelInteract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req PixelInteractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" || strings.TrimSpace(req.EntityID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id and entity_id are required"})
		return
	}
	snapshot, event, err := h.pixel.Interact(strings.TrimSpace(req.PlayerID), strings.TrimSpace(req.EntityID))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "world": snapshot, "event": event})
}

func (h *PixelHandler) handlePixelUseItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req PixelUseItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" || strings.TrimSpace(req.ItemID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id and item_id are required"})
		return
	}
	snapshot, event, err := h.pixel.UseItem(strings.TrimSpace(req.PlayerID), strings.TrimSpace(req.ItemID), req.Target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "world": snapshot, "event": event})
}

func (h *PixelHandler) handlePixelReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	snapshot, event, err := h.pixel.Reset()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "world": snapshot, "event": event})
}

func (h *PixelHandler) handlePixelEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	_, events, unsubscribe := h.pixel.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = writeSSE(w, "connected", map[string]any{"source": "pixel"})
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, event.Type, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSE(w http.ResponseWriter, eventName string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if eventName != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", string(body))
	return err
}
