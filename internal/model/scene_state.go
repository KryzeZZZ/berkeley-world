package model

type SceneState struct {
	ID      string         `json:"id"`
	Version int64          `json:"version"`
	Meta    map[string]any `json:"meta"`
}
