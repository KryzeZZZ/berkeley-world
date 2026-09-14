package scene

import (
	"strings"
)

const (
	aliasKindLayer  = "layer"
	aliasKindObject = "object"
)

func (s *SceneInstance) resolveAlias(kind, query string) (string, bool) {
	query = normalizeAliasKey(query)
	if query == "" {
		return "", false
	}
	root := s.ensureSemanticAliasRoot()
	kindMap, ok := root[kind]
	if !ok {
		return "", false
	}
	value, ok := kindMap[query]
	return value, ok
}

func (s *SceneInstance) recordAlias(kind, query, target string) {
	query = normalizeAliasKey(query)
	target = strings.TrimSpace(target)
	if query == "" || target == "" {
		return
	}
	root := s.ensureSemanticAliasRoot()
	kindMap, ok := root[kind]
	if !ok {
		kindMap = map[string]string{}
		root[kind] = kindMap
	}
	kindMap[query] = target
}

func (s *SceneInstance) ensureSemanticAliasRoot() map[string]map[string]string {
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw, ok := s.state.Meta["semantic_aliases"]
	if !ok {
		root := map[string]map[string]string{}
		s.state.Meta["semantic_aliases"] = root
		return root
	}
	if typed, ok := raw.(map[string]map[string]string); ok {
		return typed
	}
	converted := map[string]map[string]string{}
	if loose, ok := raw.(map[string]any); ok {
		for kind, v := range loose {
			inner := map[string]string{}
			if innerLoose, ok := v.(map[string]any); ok {
				for k, vv := range innerLoose {
					if s, ok := vv.(string); ok {
						inner[k] = s
					}
				}
			}
			converted[kind] = inner
		}
	}
	s.state.Meta["semantic_aliases"] = converted
	return converted
}

func normalizeAliasKey(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.Join(strings.Fields(v), " ")
	return v
}
