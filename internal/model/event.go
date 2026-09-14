package model

type Event struct {
	Type      string         `json:"type"`
	SceneID   string         `json:"scene_id"`
	PlayerID  string         `json:"player_id"`
	ObjectID  string         `json:"object_id,omitempty"`
	DiceTotal int            `json:"dice_total,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}
