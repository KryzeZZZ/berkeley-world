package scene

import (
	"fmt"
	"sort"
	"strings"
)

func (s *SceneInstance) GetNearbyLayersAndObjects(playerID string) (map[string]any, error) {
	player := s.players[playerID]
	if player == nil {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	current := s.currentLayerForPlayer(playerID)
	if strings.TrimSpace(current) == "" {
		current = s.ID
	}
	parent := s.getLayerParent(current)
	children := s.collectChildLayers(current)

	layerSet := map[string]struct{}{current: {}}
	if parent != "" {
		layerSet[parent] = struct{}{}
	}
	for _, child := range children {
		if strings.TrimSpace(child) != "" {
			layerSet[child] = struct{}{}
		}
	}

	layerObjects := make(map[string][]map[string]any, len(layerSet))
	for layerID := range layerSet {
		layerObjects[layerID] = []map[string]any{}
	}
	for _, obj := range s.objects {
		if obj == nil {
			continue
		}
		layerID, _ := obj.State["layer"].(string)
		layerID = strings.TrimSpace(layerID)
		if layerID == "" {
			continue
		}
		if _, visible := layerSet[layerID]; !visible {
			continue
		}
		layerObjects[layerID] = append(layerObjects[layerID], map[string]any{
			"id":          obj.ID,
			"name":        obj.Name,
			"layer":       layerID,
			"tags":        append([]string{}, obj.Tags...),
			"description": extractObjectDescription(obj.State),
		})
	}

	layers := make([]string, 0, len(layerSet))
	for layerID := range layerSet {
		layers = append(layers, layerID)
	}
	sort.Strings(layers)
	return map[string]any{
		"current_layer": current,
		"parent_layer":  parent,
		"child_layers":  children,
		"layers":        layers,
		"layer_objects": layerObjects,
	}, nil
}
