package actor

import "SAO/internal/model"

type SceneActor interface {
	Run()
	Enqueue(action model.QueuedAction) error
}
