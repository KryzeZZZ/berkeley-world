package scene

import "strings"

const layerDescriptionsMetaKey = "layer_descriptions"
const layerItemStatesMetaKey = "layer_item_states"
const layerBootstrappedMetaKey = "layer_bootstrapped"
const layerDescriptionStaleMetaKey = "layer_description_stale"

func (s *SceneInstance) GetLayerDescription(layerID string) string {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" || s.state.Meta == nil {
		return ""
	}
	raw := s.state.Meta[layerDescriptionsMetaKey]
	descs, ok := raw.(map[string]string)
	if ok {
		return strings.TrimSpace(descs[layerID])
	}
	loose, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := loose[layerID].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (s *SceneInstance) SetLayerDescription(layerID, description string) {
	layerID = strings.TrimSpace(layerID)
	description = sanitizeSceneDescription(description)
	if layerID == "" || description == "" {
		return
	}
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw := s.state.Meta[layerDescriptionsMetaKey]
	descs, ok := raw.(map[string]string)
	if !ok {
		descs = map[string]string{}
		if loose, ok := raw.(map[string]any); ok {
			for k, v := range loose {
				if str, ok := v.(string); ok {
					descs[k] = str
				}
			}
		}
	}
	descs[layerID] = description
	s.state.Meta[layerDescriptionsMetaKey] = descs
}

func (s *SceneInstance) SetLayerDescriptionStale(layerID string, stale bool) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw := s.state.Meta[layerDescriptionStaleMetaKey]
	flags, ok := raw.(map[string]bool)
	if !ok {
		flags = map[string]bool{}
		if loose, ok := raw.(map[string]any); ok {
			for k, v := range loose {
				if b, ok := v.(bool); ok {
					flags[k] = b
				}
			}
		}
	}
	flags[layerID] = stale
	s.state.Meta[layerDescriptionStaleMetaKey] = flags
}

func sanitizeSceneDescription(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len([]rune(v)) > 320 {
		v = string([]rune(v)[:320])
	}
	return v
}

func (s *SceneInstance) currentLayerForPlayer(playerID string) string {
	player := s.players[playerID]
	if player == nil || len(player.LayerStack) == 0 {
		return s.ID
	}
	return player.LayerStack[len(player.LayerStack)-1]
}

func (s *SceneInstance) StoreLayerItemStateSnapshot(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw := s.state.Meta[layerItemStatesMetaKey]
	layerStates, ok := raw.(map[string]any)
	if !ok {
		layerStates = map[string]any{}
	}

	items := make([]map[string]any, 0)
	for _, obj := range s.objects {
		if obj == nil || !isItemObject(obj) {
			continue
		}
		objLayer, _ := obj.State["layer"].(string)
		if strings.TrimSpace(objLayer) != layerID {
			continue
		}
		items = append(items, map[string]any{
			"id":      obj.ID,
			"name":    obj.Name,
			"version": obj.Version,
			"state":   cloneObjectState(obj.State),
		})
	}
	layerStates[layerID] = items
	s.state.Meta[layerItemStatesMetaKey] = layerStates
}

func (s *SceneInstance) IsLayerBootstrapped(layerID string) bool {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" || s.state.Meta == nil {
		return false
	}
	raw := s.state.Meta[layerBootstrappedMetaKey]
	if typed, ok := raw.(map[string]bool); ok {
		return typed[layerID]
	}
	if loose, ok := raw.(map[string]any); ok {
		if v, ok := loose[layerID].(bool); ok {
			return v
		}
	}
	return false
}

func (s *SceneInstance) MarkLayerBootstrapped(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw := s.state.Meta[layerBootstrappedMetaKey]
	flags, ok := raw.(map[string]bool)
	if !ok {
		flags = map[string]bool{}
		if loose, ok := raw.(map[string]any); ok {
			for k, v := range loose {
				if b, ok := v.(bool); ok {
					flags[k] = b
				}
			}
		}
	}
	flags[layerID] = true
	s.state.Meta[layerBootstrappedMetaKey] = flags
}
