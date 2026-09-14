package model

import "fmt"

type CreateObjectSpec struct {
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name"`
	Tags  []string       `json:"tags,omitempty"`
	State map[string]any `json:"state,omitempty"`
}

type Action struct {
	Type            string            `json:"type"`
	Interaction     string            `json:"interaction,omitempty"`
	TargetObjectID  string            `json:"target_object_id,omitempty"`
	TargetQuery     string            `json:"target_query,omitempty"`
	LayerID         string            `json:"layer_id,omitempty"`
	DiceExpr        string            `json:"dice_expr,omitempty"`
	UseAdvantage    bool              `json:"use_advantage,omitempty"`
	UseDisadvantage bool              `json:"use_disadvantage,omitempty"`
	Patch           map[string]any    `json:"patch,omitempty"`
	Payload         map[string]any    `json:"payload,omitempty"`
	CreateObject    *CreateObjectSpec `json:"create_object,omitempty"`
}

type ActionEnvelope struct {
	PlayerID string `json:"player_id"`
	Action   Action `json:"action"`
}

type QueuedAction struct {
	PlayerID string
	Action   Action
}

func (a Action) ValidateShape() error {
	if a.Type == "" {
		return fmt.Errorf("action.type is required")
	}
	if !isAllowedActionType(a.Type) {
		return fmt.Errorf("unsupported action type: %s", a.Type)
	}
	if a.UseAdvantage && a.UseDisadvantage {
		return fmt.Errorf("advantage and disadvantage cannot both be true")
	}
	if (a.Type == "push_layer" || a.Type == "replace_layer") && a.LayerID == "" {
		return fmt.Errorf("%s requires layer_id", a.Type)
	}
	if a.Type == "observe" {
		// observe can target object, layer, or current scene when no target is provided.
		return nil
	}
	if a.Type == "interact" {
		if a.Interaction != "" && !isAllowedInteraction(a.Interaction) {
			return fmt.Errorf("unsupported interaction: %s", a.Interaction)
		}
		if a.TargetObjectID == "" && a.TargetQuery == "" {
			return fmt.Errorf("interact requires target_object_id or target_query")
		}
	}
	return nil
}

func isAllowedInteraction(v string) bool {
	switch v {
	case "open_container", "pickup_item", "drop_item":
		return true
	default:
		return false
	}
}

func isAllowedActionType(v string) bool {
	switch v {
	case "push_layer", "pop_layer", "replace_layer", "interact", "observe", "narrate":
		return true
	default:
		return false
	}
}
