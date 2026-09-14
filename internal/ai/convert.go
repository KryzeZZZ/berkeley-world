package ai

import "SAO/internal/model"

func ToModelAction(parsed ParsedAction) model.Action {
	action := model.Action{
		Type:            parsed.Type,
		Interaction:     parsed.Interaction,
		TargetQuery:     parsed.TargetQuery,
		LayerID:         parsed.LayerID,
		DiceExpr:        parsed.DiceExpr,
		UseAdvantage:    parsed.UseAdvantage,
		UseDisadvantage: parsed.UseDisadvantage,
		Patch:           parsed.Patch,
		Payload:         parsed.Payload,
	}
	if parsed.CreateObject != nil {
		action.CreateObject = &model.CreateObjectSpec{
			Name:  parsed.CreateObject.Name,
			Tags:  append([]string{}, parsed.CreateObject.Tags...),
			State: parsed.CreateObject.State,
		}
	}
	return action
}
