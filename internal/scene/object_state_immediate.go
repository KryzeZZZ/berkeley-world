package scene

import (
	"fmt"
	"strings"
)

func setImmediateObjectDescription(objName string, state map[string]any) bool {
	if state == nil {
		return false
	}
	desc := immediateObjectDescription(objName, state)
	if desc == "" {
		return false
	}
	state["description"] = desc
	return true
}

func immediateObserveNarration(objName string, state map[string]any) (string, bool) {
	desc := immediateObjectDescription(objName, state)
	if desc == "" {
		return "", false
	}
	return desc, true
}

func immediateObjectDescription(objName string, state map[string]any) string {
	name := strings.TrimSpace(objName)
	if name == "" {
		name = "该物品"
	}
	if isTruthyState(state, "destroyed", "is_destroyed") {
		return fmt.Sprintf("%s已被破坏，当前不可用。", name)
	}
	if isTruthyState(state, "torn", "broken", "ruined") {
		return fmt.Sprintf("%s已受损，当前不可用。", name)
	}
	if isTruthyState(state, "burned", "consumed") {
		return fmt.Sprintf("%s已经损坏，无法正常使用。", name)
	}
	if holderID := strings.TrimSpace(firstNonEmptyString(state["holder_player_id"], state["owner_player_id"])); holderID != "" {
		return fmt.Sprintf("%s已被玩家携带中。", name)
	}
	if layerID := strings.TrimSpace(firstNonEmptyString(state["layer"])); layerID == "背包" {
		return fmt.Sprintf("%s在背包中。", name)
	}
	if isTruthyState(state, "opened", "is_open") {
		return fmt.Sprintf("%s已被打开。", name)
	}
	if isTruthyState(state, "locked", "is_locked") {
		return fmt.Sprintf("%s处于上锁状态。", name)
	}
	return ""
}

func isTruthyState(state map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := state[key]
		if !ok {
			continue
		}
		if truthy(value) {
			return true
		}
	}
	return false
}

func truthy(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(typed))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}
