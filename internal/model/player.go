package model

type PlayerState struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	SceneID         string               `json:"scene_id"`
	LayerStack      []string             `json:"layer_stack"`
	InventoryObjIDs []string             `json:"inventory_obj_ids"`
	Attributes      PlayerAttributePanel `json:"attributes"`
	DeviceID        string               `json:"device_id,omitempty"`
	DeviceLocked    bool                 `json:"device_locked,omitempty"`
}

type PlayerAttributePanel struct {
	Strength     int `json:"strength"`
	Dexterity    int `json:"dexterity"`
	Constitution int `json:"constitution"`
	Intelligence int `json:"intelligence"`
	Wisdom       int `json:"wisdom"`
	Charisma     int `json:"charisma"`
	Luck         int `json:"luck"`
}

func DefaultPlayerAttributePanel() PlayerAttributePanel {
	return PlayerAttributePanel{
		Strength:     10,
		Dexterity:    10,
		Constitution: 10,
		Intelligence: 10,
		Wisdom:       10,
		Charisma:     10,
		Luck:         10,
	}
}

func (p PlayerAttributePanel) IsZero() bool {
	return p.Strength == 0 &&
		p.Dexterity == 0 &&
		p.Constitution == 0 &&
		p.Intelligence == 0 &&
		p.Wisdom == 0 &&
		p.Charisma == 0 &&
		p.Luck == 0
}

func (p *PlayerAttributePanel) Normalize() {
	if p == nil {
		return
	}
	if p.IsZero() {
		*p = DefaultPlayerAttributePanel()
		return
	}
	p.Strength = normalizeAttrScore(p.Strength)
	p.Dexterity = normalizeAttrScore(p.Dexterity)
	p.Constitution = normalizeAttrScore(p.Constitution)
	p.Intelligence = normalizeAttrScore(p.Intelligence)
	p.Wisdom = normalizeAttrScore(p.Wisdom)
	p.Charisma = normalizeAttrScore(p.Charisma)
	p.Luck = normalizeAttrScore(p.Luck)
}

func normalizeAttrScore(v int) int {
	if v <= 0 {
		return 10
	}
	if v > 30 {
		return 30
	}
	return v
}

func ScoreModifier(score int) int {
	return (score - 10) / 2
}
