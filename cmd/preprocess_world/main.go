package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"SAO/internal/config"
	"SAO/internal/model"
	"SAO/internal/scene"
)

type persistedWorld struct {
	PlayerToScene map[string]string `json:"player_to_scene"`
	Scenes        []sceneSnapshot   `json:"scenes"`
}

type sceneSnapshot struct {
	ID         string               `json:"id"`
	State      model.SceneState     `json:"state"`
	LayerStack []string             `json:"layer_stack"`
	Players    []*model.PlayerState `json:"players"`
	Objects    []*model.GameObject  `json:"objects"`
}

func main() {
	_ = config.LoadEnvFile(envFilePath())

	path := strings.TrimSpace(os.Getenv("WORLD_JSON_PATH"))
	if path == "" {
		path = "data/world.json"
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("read world json failed: %v\n", err)
		os.Exit(1)
	}
	raw = stripUTF8BOM(raw)

	var worldData persistedWorld
	if err := json.Unmarshal(raw, &worldData); err != nil {
		fmt.Printf("parse world json failed: %v\n", err)
		os.Exit(1)
	}

	totalLayers := 0
	totalObjects := 0
	for _, snap := range worldData.Scenes {
		totalLayers += len(collectLayerIDs(snap))
		for _, obj := range snap.Objects {
			if obj == nil {
				continue
			}
			if strings.TrimSpace(obj.ID) == "" {
				continue
			}
			totalObjects++
		}
	}
	totalWork := totalLayers + totalObjects
	if totalWork == 0 {
		totalWork = 1
	}
	progressEvery := totalWork / 50
	if progressEvery < 1 {
		progressEvery = 1
	}
	done := 0
	renderProgress := func(force bool) {
		if !force && done%progressEvery != 0 {
			return
		}
		percent := (done * 100) / totalWork
		if percent > 100 {
			percent = 100
		}
		fmt.Printf("\rprogress: %d%% (%d/%d)", percent, done, totalWork)
	}

	for i := range worldData.Scenes {
		snap := &worldData.Scenes[i]
		sceneID := strings.TrimSpace(snap.ID)
		if sceneID == "" {
			continue
		}
		instance := scene.NewSceneInstance(sceneID, 64, nil)
		instance.Restore(scene.SceneSnapshot{
			ID:         snap.ID,
			State:      snap.State,
			LayerStack: append([]string{}, snap.LayerStack...),
			Players:    clonePlayers(snap.Players),
			Objects:    cloneObjects(snap.Objects),
		})

		layerIDs := collectLayerIDs(*snap)
		for _, layerID := range layerIDs {
			if _, err := instance.RefreshLayerDescription(layerID); err != nil {
				fmt.Printf("refresh layer description failed (scene=%s layer=%s): %v\n", sceneID, layerID, err)
			}
			done++
			renderProgress(false)
		}
		for _, obj := range snap.Objects {
			if obj == nil {
				continue
			}
			objID := strings.TrimSpace(obj.ID)
			if objID == "" {
				continue
			}
			if _, err := instance.RefreshObjectDescription(objID); err != nil {
				fmt.Printf("refresh object description failed (scene=%s obj=%s): %v\n", sceneID, objID, err)
			}
			done++
			renderProgress(false)
		}

		updated := instance.Snapshot()
		snap.State = updated.State
		snap.LayerStack = append([]string{}, updated.LayerStack...)
		snap.Players = clonePlayers(updated.Players)
		snap.Objects = cloneObjects(updated.Objects)
	}

	out, err := json.MarshalIndent(worldData, "", "  ")
	if err != nil {
		fmt.Printf("marshal world json failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Printf("write world json failed: %v\n", err)
		os.Exit(1)
	}

	renderProgress(true)
	fmt.Println()
	fmt.Printf("world descriptions refreshed: %s\n", path)
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func clonePlayers(players []*model.PlayerState) []*model.PlayerState {
	out := make([]*model.PlayerState, 0, len(players))
	for _, player := range players {
		if player == nil {
			continue
		}
		cloned := &model.PlayerState{
			ID:           player.ID,
			Name:         player.Name,
			SceneID:      player.SceneID,
			Attributes:   player.Attributes,
			DeviceID:     player.DeviceID,
			DeviceLocked: player.DeviceLocked,
		}
		cloned.Attributes.Normalize()
		if len(player.LayerStack) > 0 {
			cloned.LayerStack = append([]string{}, player.LayerStack...)
		}
		if len(player.InventoryObjIDs) > 0 {
			cloned.InventoryObjIDs = append([]string{}, player.InventoryObjIDs...)
		}
		out = append(out, cloned)
	}
	return out
}

func cloneObjects(objects []*model.GameObject) []*model.GameObject {
	out := make([]*model.GameObject, 0, len(objects))
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		cloned := &model.GameObject{
			ID:      obj.ID,
			Name:    obj.Name,
			Version: obj.Version,
		}
		if len(obj.Tags) > 0 {
			cloned.Tags = append([]string{}, obj.Tags...)
		}
		if obj.State != nil {
			cloned.State = map[string]any{}
			for key, value := range obj.State {
				cloned.State[key] = value
			}
		}
		out = append(out, cloned)
	}
	return out
}

func collectLayerIDs(snap sceneSnapshot) []string {
	set := map[string]struct{}{}
	if strings.TrimSpace(snap.ID) != "" {
		set[snap.ID] = struct{}{}
	}
	for _, layerID := range snap.LayerStack {
		if strings.TrimSpace(layerID) != "" {
			set[layerID] = struct{}{}
		}
	}
	for _, player := range snap.Players {
		if player == nil {
			continue
		}
		for _, layerID := range player.LayerStack {
			if strings.TrimSpace(layerID) != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	for _, obj := range snap.Objects {
		if obj == nil {
			continue
		}
		if layerID, ok := obj.State["layer"].(string); ok {
			if strings.TrimSpace(layerID) != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	layerDescs := asStringMap(snap.State.Meta, "layer_descriptions")
	for layerID := range layerDescs {
		if strings.TrimSpace(layerID) != "" {
			set[layerID] = struct{}{}
		}
	}
	layerParents := asStringMap(snap.State.Meta, "layer_parents")
	for child, parent := range layerParents {
		if strings.TrimSpace(child) != "" {
			set[child] = struct{}{}
		}
		if strings.TrimSpace(parent) != "" {
			set[parent] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for layerID := range set {
		out = append(out, layerID)
	}
	sort.Strings(out)
	return out
}

func asStringMap(meta map[string]any, key string) map[string]string {
	out := map[string]string{}
	if meta == nil {
		return out
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}
