package scene

import (
	"strings"

	"SAO/internal/model"
)

func inferRollAttribute(queued model.QueuedAction) string {
	if queued.Action.Payload != nil {
		if raw, _ := queued.Action.Payload["attribute"].(string); strings.TrimSpace(raw) != "" {
			if key := normalizeAttributeKey(raw); key != "" {
				return key
			}
		}
	}
	switch queued.Action.Type {
	case "observe":
		return "wisdom"
	case "interact":
		switch strings.TrimSpace(queued.Action.Interaction) {
		case "pickup_item", "drop_item", "open_container":
			return "strength"
		default:
			return "dexterity"
		}
	default:
		return "luck"
	}
}

func (s *SceneInstance) inventoryRollModifier(player *model.PlayerState) (int, []string) {
	if player == nil || len(player.InventoryObjIDs) == 0 {
		return 0, nil
	}
	total := 0
	names := make([]string, 0, len(player.InventoryObjIDs))
	for _, objID := range player.InventoryObjIDs {
		obj := s.objects[objID]
		if obj == nil {
			continue
		}
		names = append(names, strings.TrimSpace(obj.Name))
		if obj.State != nil {
			if bonus, ok := anyToInt(obj.State["dice_bonus"]); ok {
				total += bonus
			}
		}
		for _, tag := range obj.Tags {
			tagLower := strings.ToLower(strings.TrimSpace(tag))
			switch tagLower {
			case "lucky", "luck", "blessed", "charm", "幸运", "护符":
				total += 1
			case "cursed", "curse", "诅咒":
				total -= 2
			}
		}
	}
	if total > 8 {
		total = 8
	}
	if total < -8 {
		total = -8
	}
	return total, names
}

func anyToInt(v any) (int, bool) {
	switch typed := v.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	default:
		return 0, false
	}
}
