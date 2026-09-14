package scene

import (
	"fmt"
	"strings"

	"SAO/internal/ai"
)

func (s *SceneInstance) ResolveOrCreateLayerForTransition(playerID, query, actionType, rawInput string) (string, bool, error) {
	layerID, err := s.ResolveLayerID(playerID, query)
	if err == nil {
		return layerID, false, nil
	}
	if !s.AllowLayerCreation() {
		return "", false, err
	}
	player := s.players[playerID]
	playerLayers := []string{}
	currentLayer := s.ID
	if player != nil {
		playerLayers = append(playerLayers, player.LayerStack...)
		if len(playerLayers) > 0 {
			currentLayer = playerLayers[len(playerLayers)-1]
		}
	}
	known := s.collectLayerCandidates(playerID)
	plan, planErr := ai.CallAILayerPlanner(ai.LayerPlanInput{
		RawInput:     rawInput,
		Requested:    query,
		ActionType:   actionType,
		CurrentLayer: currentLayer,
		PlayerLayers: playerLayers,
		KnownLayers:  known,
	})
	if planErr != nil {
		return "", false, err
	}
	layerID = strings.TrimSpace(plan.LayerID)
	if layerID == "" {
		return "", false, fmt.Errorf("layer planner returned empty layer_id")
	}
	if _, exists := s.knownLayers[layerID]; exists {
		return layerID, false, nil
	}
	s.RegisterLayer(layerID)
	s.applyLayerRelation(currentLayer, layerID, plan.Relation)
	return layerID, true, nil
}

func (s *SceneInstance) CreateLayerForPlayer(playerID, layerID, relation, parentLayerID string) (string, error) {
	if !s.AllowLayerCreation() {
		return "", fmt.Errorf("scene creation is disabled by policy")
	}
	player := s.players[playerID]
	if player == nil {
		return "", fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return "", fmt.Errorf("layer_id is required")
	}
	if _, exists := s.knownLayers[layerID]; exists {
		return layerID, nil
	}
	currentLayer := s.currentLayerForPlayer(playerID)
	parent := strings.TrimSpace(parentLayerID)
	if parent != "" {
		currentLayer = parent
	}
	s.RegisterLayer(layerID)
	s.applyLayerRelation(currentLayer, layerID, relation)
	return layerID, nil
}

func (s *SceneInstance) applyLayerRelation(currentLayer, layerID, relation string) {
	currentLayer = strings.TrimSpace(currentLayer)
	layerID = strings.TrimSpace(layerID)
	relation = strings.TrimSpace(strings.ToLower(relation))
	if currentLayer == "" || layerID == "" || currentLayer == layerID {
		return
	}
	switch relation {
	case "same":
		return
	case "parent":
		s.setLayerParent(currentLayer, layerID)
	case "sibling":
		parent := s.getLayerParent(currentLayer)
		if parent == "" {
			parent = s.ID
		}
		s.setLayerParent(layerID, parent)
	default:
		s.setLayerParent(layerID, currentLayer)
	}
}
