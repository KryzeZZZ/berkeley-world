package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

var persistMu sync.Mutex

func (m *Manager) EnablePersistence(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistPath = strings.TrimSpace(path)
}

func (m *Manager) LoadFromPersistence() (bool, error) {
	if dsn := strings.TrimSpace(os.Getenv("WORLD_PG_DSN")); dsn != "" {
		mode := strings.TrimSpace(os.Getenv("WORLD_DB_MODE"))
		if mode == "" || strings.EqualFold(mode, "structured") {
			worldData, ok, err := loadWorldFromStructuredDB(dsn, strings.TrimSpace(os.Getenv("WORLD_ID")))
			if err != nil {
				return false, err
			}
			if ok {
				return m.restoreFromSnapshot(worldData)
			}
			return false, nil
		}
		worldData, ok, err := loadWorldFromDB(dsn, strings.TrimSpace(os.Getenv("WORLD_ID")))
		if err != nil {
			return false, err
		}
		if ok {
			return m.restoreFromSnapshot(worldData)
		}
		return false, nil
	}
	m.mu.RLock()
	path := m.persistPath
	m.mu.RUnlock()
	if path == "" {
		return false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read persistence file failed: %w", err)
	}
	data = stripUTF8BOM(data)

	var worldData persistedWorld
	if err := json.Unmarshal(data, &worldData); err != nil {
		return false, fmt.Errorf("parse persistence file failed: %w", err)
	}
	return m.restoreFromSnapshot(worldData)
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func (m *Manager) persistIfEnabled() error {
	if dsn := strings.TrimSpace(os.Getenv("WORLD_PG_DSN")); dsn != "" {
		worldData := m.snapshotWorld()
		mode := strings.TrimSpace(os.Getenv("WORLD_DB_MODE"))
		if mode == "" || strings.EqualFold(mode, "structured") {
			return persistWorldToStructuredDB(dsn, strings.TrimSpace(os.Getenv("WORLD_ID")), worldData)
		}
		return persistWorldToDB(dsn, strings.TrimSpace(os.Getenv("WORLD_ID")), worldData)
	}
	m.mu.RLock()
	path := m.persistPath
	if path == "" {
		m.mu.RUnlock()
		return nil
	}
	scenes := make([]*scene.SceneInstance, 0, len(m.scenes))
	for _, instance := range m.scenes {
		scenes = append(scenes, instance)
	}
	playerToScene := make(map[string]string, len(m.playerToScene))
	for playerID, sceneID := range m.playerToScene {
		playerToScene[playerID] = sceneID
	}
	m.mu.RUnlock()

	worldData := persistedWorld{
		PlayerToScene: playerToScene,
		Scenes:        make([]sceneSnapshot, 0, len(scenes)),
	}
	for _, instance := range scenes {
		snap := instance.Snapshot()
		worldData.Scenes = append(worldData.Scenes, sceneSnapshot{
			ID:         snap.ID,
			State:      snap.State,
			LayerStack: append([]string{}, snap.LayerStack...),
			Players:    clonePlayers(snap.Players),
			Objects:    cloneObjects(snap.Objects),
		})
	}

	content, err := json.MarshalIndent(worldData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal persistence data failed: %w", err)
	}

	persistMu.Lock()
	defer persistMu.Unlock()

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create persistence directory failed: %w", err)
		}
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return fmt.Errorf("write persistence tmp file failed: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace persistence file failed: %w", err)
	}
	return nil
}

func (m *Manager) FlushPersistence() error {
	return m.persistIfEnabled()
}

func (m *Manager) snapshotWorld() persistedWorld {
	m.mu.RLock()
	scenes := make([]*scene.SceneInstance, 0, len(m.scenes))
	for _, instance := range m.scenes {
		scenes = append(scenes, instance)
	}
	playerToScene := make(map[string]string, len(m.playerToScene))
	for playerID, sceneID := range m.playerToScene {
		playerToScene[playerID] = sceneID
	}
	m.mu.RUnlock()

	worldData := persistedWorld{
		PlayerToScene: playerToScene,
		Scenes:        make([]sceneSnapshot, 0, len(scenes)),
	}
	for _, instance := range scenes {
		snap := instance.Snapshot()
		worldData.Scenes = append(worldData.Scenes, sceneSnapshot{
			ID:         snap.ID,
			State:      snap.State,
			LayerStack: append([]string{}, snap.LayerStack...),
			Players:    clonePlayers(snap.Players),
			Objects:    cloneObjects(snap.Objects),
		})
	}
	return worldData
}

func (m *Manager) restoreFromSnapshot(worldData persistedWorld) (bool, error) {
	m.mu.Lock()
	m.scenes = map[string]*scene.SceneInstance{}
	m.playerToScene = map[string]string{}
	m.mu.Unlock()

	for _, snap := range worldData.Scenes {
		instance := scene.NewSceneInstance(snap.ID, 128, nil)
		instance.RegisterLayer("scene-地牢")
		instance.Restore(scene.SceneSnapshot{
			ID:         snap.ID,
			State:      snap.State,
			LayerStack: append([]string{}, snap.LayerStack...),
			Players:    clonePlayers(snap.Players),
			Objects:    cloneObjects(snap.Objects),
		})
		m.AddScene(instance)
	}

	m.mu.Lock()
	for playerID, sceneID := range worldData.PlayerToScene {
		m.playerToScene[playerID] = sceneID
	}
	m.mu.Unlock()
	return true, nil
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
