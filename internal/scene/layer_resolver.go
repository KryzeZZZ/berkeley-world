package scene

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SAO/internal/semantic"
)

func (s *SceneInstance) ResolveLayerID(playerID, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("layer_id is required")
	}
	candidates := s.collectLayerCandidates(playerID)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no layer candidates in scene %s", s.ID)
	}
	visibleSet := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		visibleSet[candidate] = struct{}{}
	}
	if alias, ok := s.resolveAlias(aliasKindLayer, query); ok {
		if _, exists := visibleSet[alias]; exists {
			return alias, nil
		}
	}

	if exact := matchExactLayer(query, candidates); exact != "" {
		return exact, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	candidateText := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		desc := s.GetLayerDescription(candidate)
		if strings.TrimSpace(desc) == "" {
			desc = candidate
		}
		candidateText[candidate] = strings.TrimSpace(desc)
	}

	match, err := semantic.MatchCandidatesWithDB(ctx, "layer", query, candidateText)
	if err != nil {
		matcher, err := semantic.DefaultMatcher()
		if err != nil {
			return "", err
		}
		match, err = matcher.MatchLayer(ctx, query, buildSemanticCandidates(candidates))
		if err != nil {
			return "", err
		}
	}
	s.recordAlias(aliasKindLayer, query, match.CandidateID)
	return match.CandidateID, nil
}

func matchExactLayer(query string, candidates []string) string {
	query = strings.TrimSpace(query)
	// Prefer the canonical ID when the caller already supplied it.
	for _, candidate := range candidates {
		if strings.EqualFold(query, strings.TrimSpace(candidate)) {
			return candidate
		}
	}
	// Layer IDs use a storage prefix, while players naturally refer to the
	// user-facing location name (for example "铁匠铺" vs "scene-铁匠铺").
	for _, candidate := range candidates {
		canonical := strings.TrimSpace(candidate)
		displayName := strings.TrimSpace(strings.TrimPrefix(canonical, "scene-"))
		displayName = strings.TrimSpace(strings.TrimPrefix(displayName, "scene_"))
		if strings.EqualFold(query, displayName) {
			return candidate
		}
	}
	return ""
}

func buildSemanticCandidates(candidates []string) map[string][]string {
	out := make(map[string][]string, len(candidates))
	for _, candidate := range candidates {
		base := strings.TrimSpace(candidate)
		phrases := []string{base}
		noScene := strings.TrimPrefix(base, "scene-")
		noScene = strings.TrimPrefix(noScene, "scene_")
		noScene = strings.TrimSpace(noScene)
		if noScene != "" && noScene != base {
			phrases = append(phrases, noScene)
			phrases = append(phrases, strings.ReplaceAll(noScene, "-", " "))
			phrases = append(phrases, strings.ReplaceAll(noScene, "_", " "))
		}
		out[base] = dedupePhrases(phrases)
	}
	return out
}

func dedupePhrases(values []string) []string {
	set := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := set[value]; ok {
			continue
		}
		set[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *SceneInstance) collectLayerCandidates(playerID string) []string {
	set := map[string]struct{}{s.ID: {}}
	for layerID := range s.knownLayers {
		layerID = strings.TrimSpace(layerID)
		if layerID != "" {
			set[layerID] = struct{}{}
		}
	}
	if s.layerStack != nil {
		for _, layerID := range s.layerStack.Snapshot() {
			layerID = strings.TrimSpace(layerID)
			if layerID != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	if player := s.players[playerID]; player != nil {
		for _, layerID := range player.LayerStack {
			layerID = strings.TrimSpace(layerID)
			if layerID != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	for _, obj := range s.objects {
		layerID, _ := obj.State["layer"].(string)
		layerID = strings.TrimSpace(layerID)
		if layerID != "" {
			set[layerID] = struct{}{}
		}
	}
	for child, parent := range s.getLayerParents() {
		child = strings.TrimSpace(child)
		parent = strings.TrimSpace(parent)
		if child != "" {
			set[child] = struct{}{}
		}
		if parent != "" {
			set[parent] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for layerID := range set {
		out = append(out, layerID)
	}
	return out
}
