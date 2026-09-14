package scene

import "SAO/internal/model"

type LayerStack struct {
	layers []string
}

func NewLayerStack(baseLayer string) *LayerStack {
	stack := &LayerStack{}
	if baseLayer != "" {
		stack.layers = append(stack.layers, baseLayer)
	}
	return stack
}

func (l *LayerStack) PushLayer(sceneID string) {
	if sceneID == "" {
		return
	}
	l.layers = append(l.layers, sceneID)
}

func (l *LayerStack) PopLayer() string {
	if len(l.layers) == 0 {
		return ""
	}
	top := l.layers[len(l.layers)-1]
	l.layers = l.layers[:len(l.layers)-1]
	return top
}

func (l *LayerStack) ReplaceLayer(sceneID string) {
	if len(l.layers) == 0 {
		l.PushLayer(sceneID)
		return
	}
	l.layers[len(l.layers)-1] = sceneID
}

func (l *LayerStack) Snapshot() []string {
	out := make([]string, len(l.layers))
	copy(out, l.layers)
	return out
}

func (l *LayerStack) Top() string {
	if len(l.layers) == 0 {
		return ""
	}
	return l.layers[len(l.layers)-1]
}

func GetVisibleObjects(stack []string, objects map[string]*model.GameObject) []*model.GameObject {
	visibleLayers := make(map[string]struct{}, len(stack))
	for _, layerID := range stack {
		if layerID == "" {
			continue
		}
		visibleLayers[layerID] = struct{}{}
	}
	result := make([]*model.GameObject, 0)
	for _, obj := range objects {
		objLayer, _ := obj.State["layer"].(string)
		if objLayer == "" {
			result = append(result, obj)
			continue
		}
		if _, ok := visibleLayers[objLayer]; ok {
			result = append(result, obj)
		}
	}
	return result
}
