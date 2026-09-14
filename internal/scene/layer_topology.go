package scene

import "strings"

const layerParentsMetaKey = "layer_parents"

func (s *SceneInstance) getLayerParents() map[string]string {
	if s.state.Meta == nil {
		s.state.Meta = map[string]any{}
	}
	raw := s.state.Meta[layerParentsMetaKey]
	if typed, ok := raw.(map[string]string); ok {
		return typed
	}
	out := map[string]string{}
	if loose, ok := raw.(map[string]any); ok {
		for k, v := range loose {
			if p, ok := v.(string); ok {
				out[k] = p
			}
		}
	}
	s.state.Meta[layerParentsMetaKey] = out
	return out
}

func (s *SceneInstance) setLayerParent(child, parent string) {
	child = strings.TrimSpace(child)
	parent = strings.TrimSpace(parent)
	if child == "" || parent == "" || child == parent {
		return
	}
	parents := s.getLayerParents()
	parents[child] = parent
	s.state.Meta[layerParentsMetaKey] = parents
}

func (s *SceneInstance) getLayerParent(child string) string {
	child = strings.TrimSpace(child)
	if child == "" {
		return ""
	}
	parents := s.getLayerParents()
	return strings.TrimSpace(parents[child])
}

func (s *SceneInstance) collectChildLayers(parent string) []string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil
	}
	out := make([]string, 0)
	for child, p := range s.getLayerParents() {
		child = strings.TrimSpace(child)
		p = strings.TrimSpace(p)
		if child == "" || p == "" {
			continue
		}
		if p == parent {
			out = append(out, child)
		}
	}
	return out
}
