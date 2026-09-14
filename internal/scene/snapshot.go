package scene

import "SAO/internal/model"

type SceneSnapshot struct {
	ID         string               `json:"id"`
	State      model.SceneState     `json:"state"`
	LayerStack []string             `json:"layer_stack"`
	Players    []*model.PlayerState `json:"players"`
	Objects    []*model.GameObject  `json:"objects"`
}

func (s *SceneInstance) snapshotUnsafe() SceneSnapshot {
	players := make([]*model.PlayerState, 0, len(s.players))
	for _, player := range s.players {
		players = append(players, clonePlayer(player))
	}
	objects := make([]*model.GameObject, 0, len(s.objects))
	for _, obj := range s.objects {
		objects = append(objects, cloneObject(obj))
	}
	return SceneSnapshot{
		ID:         s.ID,
		State:      cloneSceneState(s.state),
		LayerStack: s.layerStack.Snapshot(),
		Players:    players,
		Objects:    objects,
	}
}

func (s *SceneInstance) Restore(snapshot SceneSnapshot) {
	s.state = cloneSceneState(snapshot.State)
	s.layerStack = NewLayerStack("")
	s.knownLayers = map[string]struct{}{s.ID: {}}
	for _, layerID := range snapshot.LayerStack {
		s.layerStack.PushLayer(layerID)
		s.RegisterLayer(layerID)
	}
	if len(snapshot.LayerStack) == 0 {
		s.layerStack.PushLayer(s.ID)
	}
	s.players = map[string]*model.PlayerState{}
	for _, player := range snapshot.Players {
		cloned := clonePlayer(player)
		if len(cloned.LayerStack) == 0 {
			cloned.LayerStack = []string{s.ID}
		}
		for _, layerID := range cloned.LayerStack {
			s.RegisterLayer(layerID)
		}
		cloned.SceneID = s.ID
		s.players[cloned.ID] = cloned
	}
	s.objects = map[string]*model.GameObject{}
	for _, obj := range snapshot.Objects {
		cloned := cloneObject(obj)
		if cloned.State == nil {
			cloned.State = map[string]any{}
		}
		ensureObjectStateDescription(cloned)
		if _, ok := cloned.State["layer"]; !ok {
			cloned.State["layer"] = s.ID
		}
		if layerID, _ := cloned.State["layer"].(string); layerID != "" {
			s.RegisterLayer(layerID)
		}
		s.objects[cloned.ID] = cloned
	}
	s.rebuildKnownLayersFromMeta()
}

func cloneSceneState(state model.SceneState) model.SceneState {
	cloned := model.SceneState{
		ID:      state.ID,
		Version: state.Version,
		Meta:    cloneAnyMap(state.Meta),
	}
	return cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case map[string]string:
			inner := make(map[string]string, len(typed))
			for k, v := range typed {
				inner[k] = v
			}
			out[key] = inner
		default:
			out[key] = value
		}
	}
	return out
}

func (s *SceneInstance) rebuildKnownLayersFromMeta() {
	if s.state.Meta == nil {
		return
	}
	if raw, ok := s.state.Meta[layerDescriptionsMetaKey]; ok {
		if typed, ok := raw.(map[string]any); ok {
			for layerID := range typed {
				s.RegisterLayer(layerID)
			}
		}
	}
}

func clonePlayer(player *model.PlayerState) *model.PlayerState {
	if player == nil {
		return nil
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
	return cloned
}

func cloneObject(obj *model.GameObject) *model.GameObject {
	if obj == nil {
		return nil
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
	return cloned
}
