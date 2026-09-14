package scene

import (
	"time"

	"SAO/internal/model"
)

type TraceStep struct {
	Name    string         `json:"name"`
	Success bool           `json:"success"`
	Detail  map[string]any `json:"detail,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type ActionTrace struct {
	Steps []TraceStep `json:"steps"`
}

func (s *SceneInstance) ExecuteActionWithTrace(queued model.QueuedAction) ActionTrace {
	trace := ActionTrace{Steps: make([]TraceStep, 0, 5)}

	validateStart := time.Now()
	if err := s.Validate(queued); err != nil {
		trace.Steps = append(trace.Steps, TraceStep{
			Name:    "Validate",
			Success: false,
			Error:   err.Error(),
			Detail:  map[string]any{"duration_ms": time.Since(validateStart).Milliseconds()},
		})
		return trace
	}
	trace.Steps = append(trace.Steps, TraceStep{Name: "Validate", Success: true, Detail: map[string]any{
		"player_id":   queued.PlayerID,
		"action_type": queued.Action.Type,
		"duration_ms": time.Since(validateStart).Milliseconds(),
	}})

	resolveStart := time.Now()
	resolved, err := s.ResolveObject(queued)
	if err != nil {
		trace.Steps = append(trace.Steps, TraceStep{
			Name:    "ResolveObject",
			Success: false,
			Error:   err.Error(),
			Detail:  map[string]any{"duration_ms": time.Since(resolveStart).Milliseconds()},
		})
		return trace
	}
	resolveDetail := map[string]any{}
	if resolved.object != nil {
		resolveDetail["object_id"] = resolved.object.ID
		resolveDetail["object_name"] = resolved.object.Name
	}
	resolveDetail["duration_ms"] = time.Since(resolveStart).Milliseconds()
	trace.Steps = append(trace.Steps, TraceStep{Name: "ResolveObject", Success: true, Detail: resolveDetail})

	rollStart := time.Now()
	rolled, err := s.RollDiceIfNeeded(queued, resolved)
	if err != nil {
		trace.Steps = append(trace.Steps, TraceStep{
			Name:    "RollDice",
			Success: false,
			Error:   err.Error(),
			Detail:  map[string]any{"duration_ms": time.Since(rollStart).Milliseconds()},
		})
		return trace
	}
	rollDetail := map[string]any{}
	if queued.Action.DiceExpr != "" {
		rollDetail["expr"] = queued.Action.DiceExpr
		rollDetail["base_total"] = rolled.baseTotal
		rollDetail["total"] = rolled.total
		rollDetail["attribute"] = rolled.attribute
		rollDetail["attribute_modifier"] = rolled.attributeMod
		rollDetail["inventory_modifier"] = rolled.inventoryMod
		rollDetail["reasonableness_modifier"] = rolled.aiReasonableMod
	}
	rollDetail["duration_ms"] = time.Since(rollStart).Milliseconds()
	trace.Steps = append(trace.Steps, TraceStep{Name: "RollDice", Success: true, Detail: rollDetail})

	applyStart := time.Now()
	target, applyPayload, err := s.ApplyStatePatch(queued, resolved, rolled)
	if err != nil {
		trace.Steps = append(trace.Steps, TraceStep{
			Name:    "ApplyStatePatch",
			Success: false,
			Error:   err.Error(),
			Detail:  map[string]any{"duration_ms": time.Since(applyStart).Milliseconds()},
		})
		return trace
	}
	applyDetail := map[string]any{"scene_version": s.state.Version}
	if target != nil {
		applyDetail["object_id"] = target.ID
		applyDetail["object_version"] = target.Version
	}
	if queued.Action.Type == "push_layer" || queued.Action.Type == "pop_layer" || queued.Action.Type == "replace_layer" {
		if stack, err := s.GetPlayerLayerStack(queued.PlayerID); err == nil {
			applyDetail["player_layer_stack"] = stack
		}
	}
	for k, v := range applyPayload {
		applyDetail[k] = v
	}
	applyDetail["duration_ms"] = time.Since(applyStart).Milliseconds()
	trace.Steps = append(trace.Steps, TraceStep{Name: "ApplyStatePatch", Success: true, Detail: applyDetail})

	if applyPayload == nil {
		applyPayload = map[string]any{}
	}
	if isSceneTransitionAction(queued.Action.Type) {
		outcomeStart := time.Now()
		targetLayerID, _ := applyPayload["resolved_layer_id"].(string)
		createdLayer, _ := applyPayload["created_layer"].(bool)
		outcomeDetail := map[string]any{"duration_ms": time.Since(outcomeStart).Milliseconds()}
		outcomePayload, outcomeErr := s.handleSceneTransitionOutcome(queued, resolved, rolled, targetLayerID, createdLayer)
		if outcomeErr != nil {
			trace.Steps = append(trace.Steps, TraceStep{
				Name:    "GenerateOutcome",
				Success: false,
				Error:   outcomeErr.Error(),
				Detail:  outcomeDetail,
			})
			applyPayload["outcome_error"] = outcomeErr.Error()
			goto AFTER_TRANSITION_OUTCOME
		}
		for k, v := range outcomePayload {
			if _, exists := applyPayload[k]; !exists {
				applyPayload[k] = v
			}
			outcomeDetail[k] = v
		}
		trace.Steps = append(trace.Steps, TraceStep{
			Name:    "GenerateOutcome",
			Success: true,
			Detail:  outcomeDetail,
		})
	AFTER_TRANSITION_OUTCOME:
	} else if !skipOutcomeGeneration(queued, applyPayload) {
		outcomeStart := time.Now()
		outcomePayload, outcomeErr := s.GenerateOutcomeAndSpawn(queued, resolved, rolled, target)
		outcomeDetail := map[string]any{"duration_ms": time.Since(outcomeStart).Milliseconds()}
		if outcomeErr != nil {
			trace.Steps = append(trace.Steps, TraceStep{
				Name:    "GenerateOutcome",
				Success: false,
				Error:   outcomeErr.Error(),
				Detail:  outcomeDetail,
			})
			applyPayload["outcome_error"] = outcomeErr.Error()
		} else {
			for k, v := range outcomePayload {
				if _, exists := applyPayload[k]; !exists {
					applyPayload[k] = v
				}
				outcomeDetail[k] = v
			}
			trace.Steps = append(trace.Steps, TraceStep{
				Name:    "GenerateOutcome",
				Success: true,
				Detail:  outcomeDetail,
			})
		}
	}
	applyPayload["ok"] = true
	emitStart := time.Now()
	s.EmitEvent(queued, target, rolled.total, applyPayload)
	eventDetail := map[string]any{"event_type": "action_resolved", "dice_total": rolled.total}
	if target != nil {
		eventDetail["object_id"] = target.ID
	}
	eventDetail["duration_ms"] = time.Since(emitStart).Milliseconds()
	trace.Steps = append(trace.Steps, TraceStep{Name: "EmitEvent", Success: true, Detail: eventDetail})

	return trace
}
