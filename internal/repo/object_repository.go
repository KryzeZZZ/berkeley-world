package repo

import "SAO/internal/model"

type ObjectRepository interface {
	SaveObject(sceneID string, obj *model.GameObject) error
	FindObjectByID(sceneID, objectID string) (*model.GameObject, error)
	FindObjectByNameOrTag(sceneID, query string) (*model.GameObject, error)
}
