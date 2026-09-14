package scene

import (
	"strings"

	"SAO/internal/model"
)

type destructiveDecision int

const (
	destructiveNone destructiveDecision = iota
	destructiveRename
	destructiveRemove
)

func decideDestructiveIntent(rawInput, objectName string, state map[string]any) destructiveDecision {
	raw := strings.ToLower(strings.TrimSpace(rawInput))
	if containsAnyKeyword(raw, "摧毁", "销毁", "粉碎", "destroy", "smash", "obliterate") {
		return destructiveRemove
	}
	if containsAnyKeyword(raw, "撕毁", "撕碎", "tear", "rip", "burn", "焚毁") {
		if isPaperLikeObject(objectName) || isPaperLikeState(state) {
			return destructiveRename
		}
		return destructiveRemove
	}
	if isTruthyState(state, "destroyed", "is_destroyed") {
		return destructiveRemove
	}
	if isTruthyState(state, "torn", "broken", "ruined") {
		return destructiveRename
	}
	return destructiveNone
}

func decideDestructiveFinal(intent destructiveDecision, narration string, state map[string]any) destructiveDecision {
	if intent == destructiveNone {
		return destructiveNone
	}
	// Explicit state wins.
	if isTruthyState(state, "destroyed", "is_destroyed") {
		return destructiveRemove
	}
	if isTruthyState(state, "torn", "broken", "ruined") {
		if intent == destructiveRemove {
			return destructiveRename
		}
		return destructiveRename
	}

	text := strings.ToLower(strings.TrimSpace(narration))
	if text == "" {
		return intent
	}
	// Partial-damage wording means keep object with damaged/renamed state.
	if containsAnyKeyword(text,
		"未完全", "没有完全", "未被完全", "裂纹", "裂痕", "摇摇欲坠", "受损", "未摧毁", "并未摧毁",
		"not completely", "partially", "crack", "cracked", "damaged", "still standing", "not destroyed") {
		if intent == destructiveRemove {
			return destructiveRename
		}
		return intent
	}
	if containsAnyKeyword(text,
		"化为碎片", "彻底摧毁", "完全摧毁", "不复存在", "已销毁",
		"completely destroyed", "totally destroyed", "shattered", "obliterated") {
		return destructiveRemove
	}
	return intent
}

func isPaperLikeObject(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return containsAnyKeyword(lower, "信封", "便签", "纸", "卷轴", "envelope", "note", "paper", "scroll")
}

func isPaperLikeState(state map[string]any) bool {
	if state == nil {
		return false
	}
	name := strings.TrimSpace(firstNonEmptyString(state["name"], state["object_name"]))
	return isPaperLikeObject(name)
}

func renameDestroyedObject(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "残骸"
	}
	if containsAnyKeyword(name, "碎片", "残骸", "破损") {
		return name
	}
	if isPaperLikeObject(name) {
		return name + "碎片"
	}
	return "破损的" + name
}

func (s *SceneInstance) removeObjectFromScene(objectID string) {
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return
	}
	delete(s.objects, objectID)
	for _, player := range s.players {
		if player == nil || len(player.InventoryObjIDs) == 0 {
			continue
		}
		player.InventoryObjIDs = removeID(player.InventoryObjIDs, objectID)
	}
}

func applyDestroyedState(obj *model.GameObject) {
	if obj == nil {
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	obj.State["destroyed"] = true
	obj.State["usable"] = false
}

func applyDamagedState(obj *model.GameObject) {
	if obj == nil {
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	obj.State["broken"] = true
	obj.State["usable"] = false
	delete(obj.State, "destroyed")
}
