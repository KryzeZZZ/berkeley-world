package scene

import (
	"fmt"
	"strings"
)

func deriveObjectStateDescription(rawInput, narration, name string) string {
	name = strings.TrimSpace(name)
	rawLower := strings.ToLower(strings.TrimSpace(rawInput))
	if containsAnyKeyword(rawLower, "open", "unlock", "打开", "开启") {
		if name != "" {
			return fmt.Sprintf("%s opened.", name)
		}
		return "Opened."
	}
	if containsAnyKeyword(rawLower, "inspect", "examine", "look", "观察", "查看", "看看") {
		if sentence := findNarrationSentenceForItem(narration, name); sentence != "" {
			return "Observed: " + normalizeStateSentence(sentence)
		}
		return "Observed."
	}
	if sentence := findNarrationSentenceForItem(narration, name); sentence != "" {
		return normalizeStateSentence(sentence)
	}
	if name != "" {
		return fmt.Sprintf("%s state updated.", name)
	}
	return "State updated."
}

func deriveSpawnedItemDescription(narration, name string) string {
	name = strings.TrimSpace(name)
	if sentence := findNarrationSentenceForItem(narration, name); sentence != "" {
		if clause := extractItemClauseFromNarration(sentence, name); clause != "" {
			return normalizeStateSentence(clause)
		}
		return normalizeStateSentence(sentence)
	}
	if name != "" {
		return fmt.Sprintf("%s appeared.", name)
	}
	return "Item appeared."
}

func normalizeStateSentence(sentence string) string {
	s := strings.TrimSpace(sentence)
	if s == "" {
		return s
	}
	prefixes := []string{
		"你看到", "你看见", "你发现", "你注意到", "你凑近", "你缓缓", "你小心", "你轻轻", "你伸出手", "你将", "你",
		"You see", "You notice", "You find", "You observe", "You reach", "You pick up", "You open", "You gently",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimSpace(strings.TrimPrefix(s, p))
			break
		}
	}
	return strings.TrimSpace(s)
}

func extractItemClauseFromNarration(sentence, name string) string {
	s := strings.TrimSpace(sentence)
	if s == "" || strings.TrimSpace(name) == "" {
		return ""
	}
	// If the sentence contains a list after a colon, split by common list separators.
	if idx := strings.Index(s, "："); idx >= 0 && idx < len(s)-1 {
		s = strings.TrimSpace(s[idx+len("："):])
	}
	separators := []string{"、", "以及", "和", "及", ",", "，"}
	parts := []string{s}
	for _, sep := range separators {
		next := make([]string, 0, len(parts))
		for _, part := range parts {
			if strings.Contains(part, sep) {
				for _, frag := range strings.Split(part, sep) {
					frag = strings.TrimSpace(frag)
					if frag != "" {
						next = append(next, frag)
					}
				}
			} else if strings.TrimSpace(part) != "" {
				next = append(next, strings.TrimSpace(part))
			}
		}
		parts = next
	}
	for _, part := range parts {
		if strings.Contains(part, name) {
			return part
		}
	}
	return ""
}
