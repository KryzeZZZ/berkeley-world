package scene

import (
	"fmt"
	"strings"

	"SAO/internal/ai"
	"SAO/internal/dice"
	"SAO/internal/model"
)

type resolveResult struct {
	player *model.PlayerState
	object *model.GameObject
}

type rollResult struct {
	applied          bool
	baseTotal        int
	total            int
	attribute        string
	attributeScore   int
	attributeMod     int
	inventoryMod     int
	aiReasonableMod  int
	aiReasonableText string
}

func actionRequestID(action model.Action) string {
	if action.Payload == nil {
		return ""
	}
	v, _ := action.Payload["request_id"].(string)
	return strings.TrimSpace(v)
}

func payloadWithRequestID(action model.Action, payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if reqID := actionRequestID(action); reqID != "" {
		payload["request_id"] = reqID
	}
	return payload
}

func (s *SceneInstance) processAction(queued model.QueuedAction) {
	if err := s.Validate(queued); err != nil {
		s.EmitEvent(queued, nil, 0, payloadWithRequestID(queued.Action, map[string]any{"error": err.Error()}))
		return
	}
	resolved, err := s.ResolveObject(queued)
	if err != nil {
		s.EmitEvent(queued, nil, 0, payloadWithRequestID(queued.Action, map[string]any{"error": err.Error()}))
		return
	}
	rolled, err := s.RollDiceIfNeeded(queued, resolved)
	if err != nil {
		s.EmitEvent(queued, resolved.object, 0, payloadWithRequestID(queued.Action, map[string]any{"error": err.Error()}))
		return
	}
	target, applyPayload, err := s.ApplyStatePatch(queued, resolved, rolled)
	if err != nil {
		s.EmitEvent(queued, resolved.object, rolled.total, payloadWithRequestID(queued.Action, map[string]any{"error": err.Error()}))
		return
	}
	if applyPayload == nil {
		applyPayload = map[string]any{}
	}
	if rolled.applied {
		applyPayload["roll"] = map[string]any{
			"dice_expr":               queued.Action.DiceExpr,
			"base_total":              rolled.baseTotal,
			"final_total":             rolled.total,
			"attribute":               rolled.attribute,
			"attribute_score":         rolled.attributeScore,
			"attribute_modifier":      rolled.attributeMod,
			"inventory_modifier":      rolled.inventoryMod,
			"reasonableness_modifier": rolled.aiReasonableMod,
			"reasonableness_reason":   rolled.aiReasonableText,
		}
	}
	if isSceneTransitionAction(queued.Action.Type) {
		targetLayerID, _ := applyPayload["resolved_layer_id"].(string)
		createdLayer, _ := applyPayload["created_layer"].(bool)
		outcomePayload, err := s.handleSceneTransitionOutcome(queued, resolved, rolled, targetLayerID, createdLayer)
		if err == nil {
			for key, value := range outcomePayload {
				if _, exists := applyPayload[key]; !exists {
					applyPayload[key] = value
				}
			}
		} else {
			applyPayload["outcome_error"] = err.Error()
		}
	} else if !skipOutcomeGeneration(queued, applyPayload) {
		outcomePayload, err := s.GenerateOutcomeAndSpawn(queued, resolved, rolled, target)
		if err == nil {
			for key, value := range outcomePayload {
				if _, exists := applyPayload[key]; !exists {
					applyPayload[key] = value
				}
			}
		} else {
			applyPayload["outcome_error"] = err.Error()
		}
	}
	applyPayload["ok"] = true
	applyPayload = payloadWithRequestID(queued.Action, applyPayload)
	s.EmitEvent(queued, target, rolled.total, applyPayload)
}

func skipOutcomeGeneration(queued model.QueuedAction, payload map[string]any) bool {
	if isSceneTransitionAction(queued.Action.Type) {
		return true
	}
	if queued.Action.Type == "interact" && queued.Action.Interaction == "open_container" {
		return true
	}
	return true
}

func isSceneTransitionAction(actionType string) bool {
	return actionType == "push_layer" || actionType == "replace_layer"
}

func (s *SceneInstance) Validate(queued model.QueuedAction) error {
	if queued.PlayerID == "" {
		return fmt.Errorf("player_id is required")
	}
	if _, ok := s.players[queued.PlayerID]; !ok {
		return fmt.Errorf("player %s is not in scene %s", queued.PlayerID, s.ID)
	}
	return queued.Action.ValidateShape()
}

func (s *SceneInstance) ResolveObject(queued model.QueuedAction) (resolveResult, error) {
	player := s.players[queued.PlayerID]
	if queued.Action.TargetObjectID == "" {
		return resolveResult{player: player}, nil
	}
	obj, ok := s.objects[queued.Action.TargetObjectID]
	if !ok {
		return resolveResult{}, fmt.Errorf("target object %s not found", queued.Action.TargetObjectID)
	}
	return resolveResult{player: player, object: obj}, nil
}

func (s *SceneInstance) RollDiceIfNeeded(queued model.QueuedAction, resolved resolveResult) (rollResult, error) {
	expr := queued.Action.DiceExpr
	if expr == "" {
		return rollResult{}, nil
	}
	mode := dice.RollNormal
	if queued.Action.UseAdvantage {
		mode = dice.RollAdvantage
	}
	if queued.Action.UseDisadvantage {
		mode = dice.RollDisadvantage
	}
	baseTotal, err := s.roller.RollDiceWithMode(expr, mode)
	if err != nil {
		return rollResult{}, err
	}
	attr := inferRollAttribute(queued)
	panel := resolved.player.Attributes
	panel.Normalize()
	attrScore := panelScoreByAttr(panel, attr)
	attrMod := model.ScoreModifier(attrScore)
	invMod, invNames := s.inventoryRollModifier(resolved.player)

	targetName := ""
	if resolved.object != nil {
		targetName = resolved.object.Name
	}
	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	judge, _ := ai.JudgeRollModifier(ai.RollJudgeRequest{
		Instruction:       rawInput,
		ActionType:        queued.Action.Type,
		Interaction:       queued.Action.Interaction,
		Target:            targetName,
		Attribute:         attr,
		BaseRoll:          baseTotal,
		AttributeModifier: attrMod,
		InventoryModifier: invMod,
		CurrentLayer:      s.currentLayerForPlayer(queued.PlayerID),
		InventoryItems:    invNames,
	})
	total := baseTotal + attrMod + invMod + judge.Modifier
	return rollResult{
		applied:          true,
		baseTotal:        baseTotal,
		total:            total,
		attribute:        attr,
		attributeScore:   attrScore,
		attributeMod:     attrMod,
		inventoryMod:     invMod,
		aiReasonableMod:  judge.Modifier,
		aiReasonableText: judge.Reason,
	}, nil
}

func (s *SceneInstance) ApplyStatePatch(queued model.QueuedAction, resolved resolveResult, rolled rollResult) (*model.GameObject, map[string]any, error) {
	switch queued.Action.Type {
	case "push_layer":
		rawInput, _ := queued.Action.Payload["raw_input"].(string)
		resolvedLayerID, createdLayer, err := s.ResolveOrCreateLayerForTransition(queued.PlayerID, queued.Action.LayerID, queued.Action.Type, rawInput)
		if err != nil {
			return nil, nil, err
		}
		beforeLayer := s.currentLayerForPlayer(queued.PlayerID)
		if err := s.PushLayerForPlayer(queued.PlayerID, resolvedLayerID); err != nil {
			return nil, nil, err
		}
		afterLayer := s.currentLayerForPlayer(queued.PlayerID)
		if desc, _ := queued.Action.Payload["description"].(string); strings.TrimSpace(desc) != "" {
			s.SetLayerDescription(resolvedLayerID, desc)
		}
		s.state.Version++
		return nil, map[string]any{
			"resolved_layer_id": resolvedLayerID,
			"created_layer":     createdLayer,
			"layer_changed":     beforeLayer != afterLayer,
		}, nil
	case "pop_layer":
		if _, err := s.PopLayerForPlayer(queued.PlayerID); err != nil {
			return nil, nil, err
		}
		s.state.Version++
		return nil, nil, nil
	case "replace_layer":
		rawInput, _ := queued.Action.Payload["raw_input"].(string)
		resolvedLayerID, createdLayer, err := s.ResolveOrCreateLayerForTransition(queued.PlayerID, queued.Action.LayerID, queued.Action.Type, rawInput)
		if err != nil {
			return nil, nil, err
		}
		beforeLayer := s.currentLayerForPlayer(queued.PlayerID)
		if err := s.ReplaceLayerForPlayer(queued.PlayerID, resolvedLayerID); err != nil {
			return nil, nil, err
		}
		afterLayer := s.currentLayerForPlayer(queued.PlayerID)
		if desc, _ := queued.Action.Payload["description"].(string); strings.TrimSpace(desc) != "" {
			s.SetLayerDescription(resolvedLayerID, desc)
		}
		s.state.Version++
		return nil, map[string]any{
			"resolved_layer_id": resolvedLayerID,
			"created_layer":     createdLayer,
			"layer_changed":     beforeLayer != afterLayer,
		}, nil
	}

	if queued.Action.Type == "observe" {
		return s.handleObserveAction(queued, resolved, rolled)
	}

	if queued.Action.Type == "interact" {
		switch queued.Action.Interaction {
		case "open_container":
			return s.handleOpenContainerInteraction(queued, resolved, rolled)
		case "pickup_item":
			return s.handlePickupItem(queued.PlayerID, resolved)
		case "drop_item":
			return s.handleDropItem(queued.PlayerID, resolved)
		default:
			return s.handleGenericInteract(queued, resolved, rolled)
		}
	}

	if resolved.object == nil {
		s.state.Version++
		return nil, nil, nil
	}
	if resolved.object.State == nil {
		resolved.object.State = map[string]any{}
	}
	for key, value := range queued.Action.Patch {
		resolved.object.State[key] = value
	}
	setImmediateObjectDescription(resolved.object.Name, resolved.object.State)
	if queued.Action.DiceExpr != "" {
		resolved.object.State["last_roll"] = rolled.total
	}
	markObjectDescriptionStale(resolved.object)
	resolved.object.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, resolved.object)
	}
	layerID, _ := resolved.object.State["layer"].(string)
	s.scheduleObjectDescriptionRefreshLocked(resolved.object.ID)
	s.scheduleLayerDescriptionRefreshLocked(layerID)
	return resolved.object, nil, nil
}

func (s *SceneInstance) handleObserveAction(queued model.QueuedAction, resolved resolveResult, rolled rollResult) (*model.GameObject, map[string]any, error) {
	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	req := s.buildOutcomeRequest(queued, resolved, rolled, resolved.object)
	req.Interaction = "observe"
	req.Instruction = rawInput
	if resolved.object != nil {
		req.Target = resolved.object.Name
		if narration, ok := immediateObserveNarration(resolved.object.Name, resolved.object.State); ok {
			if resolved.object.State == nil {
				resolved.object.State = map[string]any{}
			}
			resolved.object.State["description"] = narration
			delete(resolved.object.State, "description_stale")
			resolved.object.Version++
			s.state.Version++
			if s.objectRepo != nil {
				_ = s.objectRepo.SaveObject(s.ID, resolved.object)
			}
			payload := buildObservePayload(req, narration)
			return resolved.object, payload, nil
		}
	}
	if layerID := strings.TrimSpace(queued.Action.LayerID); layerID != "" {
		req.Target = layerID
		req.Context.CurrentLayer = layerID
		req.Context.ChildLayers = s.collectChildLayers(layerID)
		req.Context.SceneDescription = s.GetLayerDescription(layerID)
		req.Context.NearbyObjects = s.collectOutcomeObjectsInLayer(layerID)
		req.Context.KnownItems = s.collectKnownItemsInLayer(layerID)
	}
	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		return nil, nil, err
	}
	if resolved.object != nil {
		if resolved.object.State == nil {
			resolved.object.State = map[string]any{}
		}
		resolved.object.State["description"] = deriveObjectStateDescription(rawInput, outcome.Narration, resolved.object.Name)
		delete(resolved.object.State, "description_stale")
		resolved.object.Version++
		s.state.Version++
		if s.objectRepo != nil {
			_ = s.objectRepo.SaveObject(s.ID, resolved.object)
		}
		if layerID, _ := resolved.object.State["layer"].(string); strings.TrimSpace(layerID) != "" {
			s.scheduleLayerDescriptionRefreshLocked(layerID)
		}
	}
	if layerID := strings.TrimSpace(queued.Action.LayerID); layerID != "" {
		s.SetLayerDescription(layerID, outcome.Narration)
		s.SetLayerDescriptionStale(layerID, false)
		s.state.Version++
	}
	payload := buildObservePayload(req, outcome.Narration)
	return resolved.object, payload, nil
}

func buildObservePayload(req ai.OutcomeRequest, narration string) map[string]any {
	visibleObjects := req.Context.NearbyObjects
	if visibleObjects == nil {
		visibleObjects = []ai.OutcomeObjectContext{}
	}
	childLayers := req.Context.ChildLayers
	if childLayers == nil {
		childLayers = []string{}
	}
	return map[string]any{
		"narration":       narration,
		"spawned":         []map[string]any{},
		"current_layer":   req.Context.CurrentLayer,
		"child_layers":    childLayers,
		"visible_objects": visibleObjects,
	}
}

func (s *SceneInstance) handleOpenContainerInteraction(queued model.QueuedAction, resolved resolveResult, rolled rollResult) (*model.GameObject, map[string]any, error) {
	if resolved.object == nil {
		return nil, nil, fmt.Errorf("interact open_container requires resolved target object")
	}
	if !isContainerObject(resolved.object) {
		return nil, nil, fmt.Errorf("target %s is not a container", resolved.object.Name)
	}
	if resolved.object.State == nil {
		resolved.object.State = map[string]any{}
	}
	if opened, _ := resolved.object.State["opened"].(bool); opened {
		rawInput, _ := queued.Action.Payload["raw_input"].(string)
		narration := "箱子已经被打开，里面空空如也。"
		resolved.object.State["description"] = deriveObjectStateDescription(rawInput, narration, resolved.object.Name)
		resolved.object.Version++
		s.state.Version++
		if s.objectRepo != nil {
			_ = s.objectRepo.SaveObject(s.ID, resolved.object)
		}
		if layerID, _ := resolved.object.State["layer"].(string); strings.TrimSpace(layerID) != "" {
			s.scheduleLayerDescriptionRefreshLocked(layerID)
		}
		return resolved.object, map[string]any{
			"narration": narration,
			"spawned":   []map[string]any{},
		}, nil
	}

	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	req := s.buildOutcomeRequest(queued, resolved, rolled, resolved.object)
	req.Interaction = "open_container"
	req.Instruction = rawInput
	req.Target = resolved.object.Name
	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		return nil, nil, err
	}
	resolved.object.State["opened"] = true
	resolved.object.State["description"] = deriveObjectStateDescription(rawInput, outcome.Narration, resolved.object.Name)
	spawned, err := s.spawnItemsFromOutcome(req, outcome, resolved.player, resolved.object, nil)
	if err != nil {
		return nil, nil, err
	}

	resolved.object.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, resolved.object)
	}
	if layerID, _ := resolved.object.State["layer"].(string); strings.TrimSpace(layerID) != "" {
		s.scheduleLayerDescriptionRefreshLocked(layerID)
	}
	return resolved.object, map[string]any{
		"narration": outcome.Narration,
		"spawned":   spawned,
	}, nil
}

func isContainerObject(obj *model.GameObject) bool {
	if obj == nil {
		return false
	}
	for _, tag := range obj.Tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "container", "chest", "box", "crate", "cabinet", "locker", "drawer", "箱子", "宝箱", "柜子", "抽屉", "容器":
			return true
		}
	}
	name := strings.ToLower(strings.TrimSpace(obj.Name))
	return strings.Contains(name, "箱") || strings.Contains(name, "柜") || strings.Contains(name, "抽屉") ||
		strings.Contains(name, "chest") || strings.Contains(name, "box") || strings.Contains(name, "crate") ||
		strings.Contains(name, "cabinet") || strings.Contains(name, "drawer") || strings.Contains(name, "locker")
}

func (s *SceneInstance) handleGenericInteract(queued model.QueuedAction, resolved resolveResult, rolled rollResult) (*model.GameObject, map[string]any, error) {
	if resolved.object == nil {
		return nil, nil, fmt.Errorf("interact requires resolved target object")
	}
	if resolved.object.State == nil {
		resolved.object.State = map[string]any{}
	}
	originalName := resolved.object.Name
	originalLayer, _ := resolved.object.State["layer"].(string)

	// Apply AI-provided patch first so state update happens before outcome narration.
	for key, value := range queued.Action.Patch {
		resolved.object.State[key] = value
	}
	setImmediateObjectDescription(resolved.object.Name, resolved.object.State)

	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	rawLower := strings.ToLower(strings.TrimSpace(rawInput))
	if containsAnyKeyword(rawLower, "打开", "开启", "open", "unlock") {
		resolved.object.State["opened"] = true
	}
	destructionIntent := decideDestructiveIntent(rawInput, resolved.object.Name, resolved.object.State)

	req := s.buildOutcomeRequest(queued, resolved, rolled, resolved.object)
	req.Interaction = "interact"
	req.Instruction = rawInput
	req.Target = resolved.object.Name
	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		return nil, nil, err
	}

	resolved.object.State["description"] = deriveObjectStateDescription(rawInput, outcome.Narration, resolved.object.Name)
	spawned, err := s.spawnItemsFromOutcome(req, outcome, resolved.player, resolved.object, nil)
	if err != nil {
		return nil, nil, err
	}

	payload := map[string]any{
		"narration": outcome.Narration,
		"spawned":   spawned,
	}
	destruction := decideDestructiveFinal(destructionIntent, outcome.Narration, resolved.object.State)
	switch destruction {
	case destructiveRemove:
		applyDestroyedState(resolved.object)
		s.removeObjectFromScene(resolved.object.ID)
		s.state.Version++
		payload["object_removed"] = true
		payload["removed_object_id"] = resolved.object.ID
		payload["removed_object_name"] = originalName
		if strings.TrimSpace(originalLayer) != "" {
			s.scheduleLayerDescriptionRefreshLocked(originalLayer)
		}
		return nil, payload, nil
	case destructiveRename:
		applyDamagedState(resolved.object)
		setImmediateObjectDescription(resolved.object.Name, resolved.object.State)
		payload["object_damaged"] = true
		payload["damaged_object_name"] = resolved.object.Name
	}

	resolved.object.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, resolved.object)
	}
	if layerID, _ := resolved.object.State["layer"].(string); strings.TrimSpace(layerID) != "" {
		s.scheduleLayerDescriptionRefreshLocked(layerID)
	}
	return resolved.object, payload, nil
}
func containsAnyKeyword(input string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(input, strings.ToLower(strings.TrimSpace(keyword))) {
			return true
		}
	}
	return false
}

func (s *SceneInstance) handlePickupItem(playerID string, resolved resolveResult) (*model.GameObject, map[string]any, error) {
	if resolved.object == nil {
		return nil, nil, fmt.Errorf("pickup_item requires resolved target object")
	}
	if resolved.object.State == nil {
		resolved.object.State = map[string]any{}
	}
	prevLayer, _ := resolved.object.State["layer"].(string)
	holder, _ := resolved.object.State["holder_player_id"].(string)
	if holder == playerID {
		return resolved.object, map[string]any{"narration": "该物品已在你的背包中。"}, nil
	}
	if holder != "" && holder != playerID {
		return nil, nil, fmt.Errorf("item is held by another player")
	}

	player := s.players[playerID]
	if player == nil {
		return nil, nil, fmt.Errorf("player %s not found", playerID)
	}
	resolved.object.State["holder_player_id"] = playerID
	resolved.object.State["layer"] = "背包"
	setImmediateObjectDescription(resolved.object.Name, resolved.object.State)
	if !containsID(player.InventoryObjIDs, resolved.object.ID) {
		player.InventoryObjIDs = append(player.InventoryObjIDs, resolved.object.ID)
	}
	markObjectDescriptionStale(resolved.object)
	resolved.object.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, resolved.object)
	}
	s.scheduleObjectDescriptionRefreshLocked(resolved.object.ID)
	if strings.TrimSpace(prevLayer) != "" {
		s.scheduleLayerDescriptionRefreshLocked(prevLayer)
	}
	return resolved.object, map[string]any{
		"narration": fmt.Sprintf("你拾取了%s，已放入背包。", resolved.object.Name),
	}, nil
}

func (s *SceneInstance) handleDropItem(playerID string, resolved resolveResult) (*model.GameObject, map[string]any, error) {
	if resolved.object == nil {
		return nil, nil, fmt.Errorf("drop_item requires resolved target object")
	}
	player := s.players[playerID]
	if player == nil {
		return nil, nil, fmt.Errorf("player %s not found", playerID)
	}
	if !containsID(player.InventoryObjIDs, resolved.object.ID) {
		return nil, nil, fmt.Errorf("item %s is not in player's inventory", resolved.object.ID)
	}
	if resolved.object.State == nil {
		resolved.object.State = map[string]any{}
	}
	delete(resolved.object.State, "holder_player_id")
	layerID := s.ID
	if len(player.LayerStack) > 0 {
		layerID = player.LayerStack[len(player.LayerStack)-1]
	}
	resolved.object.State["layer"] = layerID
	setImmediateObjectDescription(resolved.object.Name, resolved.object.State)
	player.InventoryObjIDs = removeID(player.InventoryObjIDs, resolved.object.ID)
	markObjectDescriptionStale(resolved.object)
	resolved.object.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, resolved.object)
	}
	s.scheduleObjectDescriptionRefreshLocked(resolved.object.ID)
	s.scheduleLayerDescriptionRefreshLocked(layerID)
	return resolved.object, map[string]any{
		"narration": fmt.Sprintf("你丢弃了%s。", resolved.object.Name),
	}, nil
}

func containsID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func removeID(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func (s *SceneInstance) EmitEvent(queued model.QueuedAction, target *model.GameObject, diceTotal int, payload map[string]any) {
	event := model.Event{
		Type:      "action_resolved",
		SceneID:   s.ID,
		PlayerID:  queued.PlayerID,
		DiceTotal: diceTotal,
		Payload:   payload,
	}
	if target != nil {
		event.ObjectID = target.ID
	}
	select {
	case s.eventStream <- event:
	default:
	}
}

func (s *SceneInstance) createObjectFromAction(spec *model.CreateObjectSpec, player *model.PlayerState) *model.GameObject {
	layerID := s.ID
	if player != nil && len(player.LayerStack) > 0 {
		layerID = player.LayerStack[len(player.LayerStack)-1]
	}
	state := map[string]any{"layer": layerID}
	if spec.State != nil {
		for key, value := range spec.State {
			state[key] = value
		}
	}
	objectID := spec.ID
	if objectID == "" {
		objectID = fmt.Sprintf("obj-%s-%d", normalizeForID(spec.Name), s.state.Version+1)
	}
	return &model.GameObject{
		ID:      objectID,
		Name:    spec.Name,
		Tags:    append([]string{}, spec.Tags...),
		State:   state,
		Version: 1,
	}
}

func normalizeForID(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "generated"
	}
	return name
}
