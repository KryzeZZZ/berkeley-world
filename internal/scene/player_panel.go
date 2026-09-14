package scene

import (
	"fmt"
	"strings"

	"SAO/internal/model"
)

func (s *SceneInstance) GetPlayerAttributePanel(playerID string) (model.PlayerAttributePanel, error) {
	player, ok := s.players[playerID]
	if !ok || player == nil {
		return model.PlayerAttributePanel{}, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	player.Attributes.Normalize()
	return player.Attributes, nil
}

func (s *SceneInstance) UpdatePlayerAttributePanel(playerID string, updates map[string]int) (model.PlayerAttributePanel, error) {
	player, ok := s.players[playerID]
	if !ok || player == nil {
		return model.PlayerAttributePanel{}, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	panel := player.Attributes
	panel.Normalize()
	for rawKey, value := range updates {
		key := normalizeAttributeKey(rawKey)
		switch key {
		case "strength":
			panel.Strength = value
		case "dexterity":
			panel.Dexterity = value
		case "constitution":
			panel.Constitution = value
		case "intelligence":
			panel.Intelligence = value
		case "wisdom":
			panel.Wisdom = value
		case "charisma":
			panel.Charisma = value
		case "luck":
			panel.Luck = value
		}
	}
	panel.Normalize()
	player.Attributes = panel
	return panel, nil
}

func normalizeAttributeKey(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "str", "strength", "力量":
		return "strength"
	case "dex", "dexterity", "敏捷":
		return "dexterity"
	case "con", "constitution", "体质":
		return "constitution"
	case "int", "intelligence", "智力":
		return "intelligence"
	case "wis", "wisdom", "感知":
		return "wisdom"
	case "cha", "charisma", "魅力":
		return "charisma"
	case "luck", "幸运":
		return "luck"
	default:
		return ""
	}
}

func panelScoreByAttr(panel model.PlayerAttributePanel, attr string) int {
	switch normalizeAttributeKey(attr) {
	case "strength":
		return panel.Strength
	case "dexterity":
		return panel.Dexterity
	case "constitution":
		return panel.Constitution
	case "intelligence":
		return panel.Intelligence
	case "wisdom":
		return panel.Wisdom
	case "charisma":
		return panel.Charisma
	case "luck":
		return panel.Luck
	default:
		return 10
	}
}
