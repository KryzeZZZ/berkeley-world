package scene

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SAO/internal/model"
	"SAO/internal/semantic"
)

func (s *SceneInstance) ResolveVisibleObjectByNameOrTag(playerID, query string) (*model.GameObject, error) {
	player, ok := s.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	normalized := normalizeQuery(query)
	if normalized == "" {
		return nil, fmt.Errorf("query is required")
	}
	visible := GetVisibleObjects(player.LayerStack, s.objects)
	if len(visible) == 0 {
		return nil, fmt.Errorf("no visible objects for player %s", playerID)
	}
	visibleByID := make(map[string]*model.GameObject, len(visible))
	for _, obj := range visible {
		visibleByID[obj.ID] = obj
	}
	if aliasID, ok := s.resolveAlias(aliasKindObject, query); ok {
		if aliased := visibleByID[aliasID]; aliased != nil {
			return aliased, nil
		}
	}

	if exact := findExactVisibleObject(query, visible); exact != nil {
		return exact, nil
	}

	byID := make(map[string]*model.GameObject, len(visible))
	candidateText := make(map[string]string, len(visible))
	for _, obj := range visible {
		byID[obj.ID] = obj
		desc := ""
		if obj.State != nil {
			if v, ok := obj.State["description"].(string); ok {
				desc = v
			}
		}
		candidateText[obj.ID] = strings.TrimSpace(fmt.Sprintf("%s | %s | %s", obj.Name, desc, strings.Join(obj.Tags, ", ")))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	match, err := semantic.MatchCandidatesWithDB(ctx, "object", query, candidateText)
	if err != nil {
		matcher, err := semantic.DefaultMatcher()
		if err != nil {
			return nil, err
		}
		match, err = matcher.MatchCandidates(ctx, query, buildSemanticCandidatesFromObjects(visible))
		if err != nil {
			return nil, err
		}
	}
	resolved := byID[match.CandidateID]
	if resolved == nil {
		return nil, fmt.Errorf("matched object %s not found in visible set", match.CandidateID)
	}
	s.recordAlias(aliasKindObject, query, resolved.ID)
	return resolved, nil
}

func findExactVisibleObject(query string, visible []*model.GameObject) *model.GameObject {
	query = strings.TrimSpace(query)
	for _, obj := range visible {
		if query == strings.TrimSpace(obj.ID) || query == strings.TrimSpace(obj.Name) {
			return obj
		}
		for _, tag := range obj.Tags {
			if query == strings.TrimSpace(tag) {
				return obj
			}
		}
	}
	return nil
}

func buildObjectPhrases(obj *model.GameObject) []string {
	phrases := make([]string, 0, len(obj.Tags)+3)
	id := strings.TrimSpace(obj.ID)
	name := strings.TrimSpace(obj.Name)
	if id != "" {
		phrases = append(phrases, id)
		phrases = append(phrases, strings.ReplaceAll(id, "-", " "))
		phrases = append(phrases, strings.ReplaceAll(id, "_", " "))
	}
	if name != "" {
		phrases = append(phrases, name)
	}
	for _, tag := range obj.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			phrases = append(phrases, tag)
		}
	}
	return dedupePhrases(phrases)
}

func buildSemanticCandidatesFromObjects(visible []*model.GameObject) map[string][]string {
	candidates := make(map[string][]string, len(visible))
	for _, obj := range visible {
		candidates[obj.ID] = buildObjectPhrases(obj)
	}
	return candidates
}

func normalizeQuery(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", " ")
	return strings.Join(strings.Fields(value), " ")
}
