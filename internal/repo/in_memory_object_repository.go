package repo

import (
	"fmt"
	"strings"
	"sync"

	"SAO/internal/model"
)

type InMemoryObjectRepository struct {
	mu      sync.RWMutex
	objects map[string]map[string]*model.GameObject
}

func NewInMemoryObjectRepository() *InMemoryObjectRepository {
	return &InMemoryObjectRepository{objects: map[string]map[string]*model.GameObject{}}
}

func (r *InMemoryObjectRepository) SaveObject(sceneID string, obj *model.GameObject) error {
	if sceneID == "" {
		return fmt.Errorf("sceneID is required")
	}
	if obj == nil || obj.ID == "" {
		return fmt.Errorf("object with id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objects[sceneID]; !ok {
		r.objects[sceneID] = map[string]*model.GameObject{}
	}
	r.objects[sceneID][obj.ID] = cloneObject(obj)
	return nil
}

func (r *InMemoryObjectRepository) FindObjectByID(sceneID, objectID string) (*model.GameObject, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sceneObjects := r.objects[sceneID]
	if sceneObjects == nil {
		return nil, fmt.Errorf("scene %s not found", sceneID)
	}
	obj := sceneObjects[objectID]
	if obj == nil {
		return nil, fmt.Errorf("object %s not found", objectID)
	}
	return cloneObject(obj), nil
}

func (r *InMemoryObjectRepository) FindObjectByNameOrTag(sceneID, query string) (*model.GameObject, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sceneObjects := r.objects[sceneID]
	if sceneObjects == nil {
		return nil, fmt.Errorf("scene %s not found", sceneID)
	}
	normalized := normalize(query)
	if normalized == "" {
		return nil, fmt.Errorf("query is required")
	}
	for _, obj := range sceneObjects {
		if normalize(obj.Name) == normalized {
			return cloneObject(obj), nil
		}
	}
	for _, obj := range sceneObjects {
		name := normalize(obj.Name)
		if strings.Contains(name, normalized) || strings.Contains(normalized, name) {
			return cloneObject(obj), nil
		}
		for _, tag := range obj.Tags {
			if normalize(tag) == normalized {
				return cloneObject(obj), nil
			}
		}
	}
	return nil, fmt.Errorf("object query %q not found", query)
}

func cloneObject(obj *model.GameObject) *model.GameObject {
	if obj == nil {
		return nil
	}
	copied := &model.GameObject{
		ID:      obj.ID,
		Name:    obj.Name,
		Version: obj.Version,
	}
	if len(obj.Tags) > 0 {
		copied.Tags = append([]string{}, obj.Tags...)
	}
	if obj.State != nil {
		copied.State = map[string]any{}
		for k, v := range obj.State {
			copied.State[k] = v
		}
	}
	return copied
}

func normalize(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", " ")
	return strings.Join(strings.Fields(value), " ")
}
