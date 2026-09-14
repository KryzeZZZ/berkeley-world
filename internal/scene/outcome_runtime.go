package scene

import (
	"fmt"
	"strings"

	"SAO/internal/ai"
	"SAO/internal/model"
)

func (s *SceneInstance) GenerateOutcomeAndSpawn(queued model.QueuedAction, resolved resolveResult, rolled rollResult, target *model.GameObject) (map[string]any, error) {
	outcomeReq := s.buildOutcomeRequest(queued, resolved, rolled, target)
	outcome, err := ai.CallAIOutcomeNarrator(outcomeReq)
	if err != nil {
		return nil, err
	}
	s.SetLayerDescription(s.currentLayerForPlayer(queued.PlayerID), outcome.Narration)
	spawned, err := s.spawnItemsFromOutcome(outcomeReq, outcome, resolved.player, target, nil)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"narration": outcome.Narration,
		"spawned":   spawned,
	}
	return payload, nil
}

func (s *SceneInstance) handleSceneTransitionOutcome(queued model.QueuedAction, resolved resolveResult, rolled rollResult, targetLayerID string, createdLayer bool) (map[string]any, error) {
	layerID := strings.TrimSpace(targetLayerID)
	if layerID == "" {
		layerID = s.currentLayerForPlayer(queued.PlayerID)
	}
	existingDesc := s.GetLayerDescription(layerID)
	isNewLayer := createdLayer || !s.IsLayerBootstrapped(layerID)

	req := s.buildOutcomeRequest(queued, resolved, rolled, nil)
	req.Target = layerID
	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	if strings.TrimSpace(rawInput) == "" {
		rawInput = queued.Action.Type
	}
	if isNewLayer {
		req.Interaction = "scene_bootstrap"
		req.Instruction = rawInput + "\n首次进入该场景，请扫描并给出关键可交互物品（不只限可拾取）。"
		req.Context.KnownItems = s.collectKnownItemsInLayer(s.previousLayerForPlayer(queued.PlayerID, layerID))
	} else {
		req.Interaction = "scene_refresh_no_loot"
		req.Instruction = rawInput + "\n仅刷新场景描述，必须返回空物品列表。仅参考当前层、当前层子层ID和当前层可见对象。"
		req.Context.SceneDescription = existingDesc
		req.Context.KnownItems = s.collectKnownItemsInLayer(layerID)
	}

	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		return nil, err
	}
	s.SetLayerDescription(layerID, outcome.Narration)
	s.StoreLayerItemStateSnapshot(layerID)

	payload := map[string]any{
		"scene_layer":                 layerID,
		"scene_description_refreshed": true,
		"scene_was_new":               isNewLayer,
		"narration":                   outcome.Narration,
	}
	if !isNewLayer {
		payload["spawned"] = []map[string]any{}
		return payload, nil
	}

	itemLayerHints, createdLayers := s.planOutcomeLayerPlacement(req, outcome.Narration, layerID)
	spawned, err := s.spawnItemsFromOutcome(req, outcome, resolved.player, nil, itemLayerHints)
	if err != nil {
		return nil, err
	}
	s.StoreLayerItemStateSnapshot(layerID)
	for _, layer := range createdLayers {
		if layerIDValue, _ := layer["layer_id"].(string); strings.TrimSpace(layerIDValue) != "" {
			s.StoreLayerItemStateSnapshot(layerIDValue)
			// Child layers already seeded by parent bootstrap should not bootstrap again on first enter.
			s.MarkLayerBootstrapped(layerIDValue)
		}
	}
	s.MarkLayerBootstrapped(layerID)
	payload["spawned"] = spawned
	if len(createdLayers) > 0 {
		payload["spawned_layers"] = createdLayers
	}
	return payload, nil
}

func (s *SceneInstance) buildOutcomeRequest(queued model.QueuedAction, resolved resolveResult, rolled rollResult, target *model.GameObject) ai.OutcomeRequest {
	interaction := strings.TrimSpace(queued.Action.Type)
	if queued.Action.Interaction != "" {
		interaction = queued.Action.Interaction
	}
	targetName := ""
	if target != nil {
		targetName = target.Name
	}
	rawInput, _ := queued.Action.Payload["raw_input"].(string)
	if strings.TrimSpace(rawInput) == "" {
		rawInput = interaction
	}
	req := ai.OutcomeRequest{
		Interaction: interaction,
		Instruction: rawInput,
		Target:      targetName,
		DiceExpr:    queued.Action.DiceExpr,
		DiceTotal:   rolled.total,
	}
	currentLayer := s.currentLayerForPlayer(queued.PlayerID)
	playerLayers := []string{}
	if resolved.player != nil && len(resolved.player.LayerStack) > 0 {
		playerLayers = append(playerLayers, resolved.player.LayerStack...)
	}
	req.Context = ai.OutcomeContext{
		SceneID:          s.ID,
		CurrentLayer:     currentLayer,
		ChildLayers:      s.collectChildLayers(currentLayer),
		SceneDescription: s.GetLayerDescription(currentLayer),
		PlayerID:         queued.PlayerID,
		PlayerLayers:     playerLayers,
		NearbyObjects:    s.collectOutcomeNearbyObjects(queued.PlayerID),
		KnownItems:       s.collectKnownItemsInLayer(currentLayer),
	}
	s.StoreLayerItemStateSnapshot(currentLayer)
	return req
}

func (s *SceneInstance) collectOutcomeNearbyObjects(playerID string) []ai.OutcomeObjectContext {
	currentLayer := strings.TrimSpace(s.currentLayerForPlayer(playerID))
	if currentLayer == "" {
		currentLayer = s.ID
	}
	out := make([]ai.OutcomeObjectContext, 0, len(s.objects))
	seen := map[string]struct{}{}
	for _, obj := range s.objects {
		if obj == nil {
			continue
		}
		layerID, _ := obj.State["layer"].(string)
		layerID = strings.TrimSpace(layerID)
		if layerID != "" {
			if layerID != currentLayer {
				continue
			}
		}
		if _, ok := seen[obj.ID]; ok {
			continue
		}
		seen[obj.ID] = struct{}{}
		out = append(out, ai.OutcomeObjectContext{
			ID:          obj.ID,
			Name:        obj.Name,
			Tags:        append([]string{}, obj.Tags...),
			Layer:       layerID,
			State:       cloneObjectState(obj.State),
			Description: extractObjectDescription(obj.State),
		})
	}
	return out
}

func (s *SceneInstance) collectOutcomeObjectsInLayer(layerID string) []ai.OutcomeObjectContext {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return nil
	}
	out := make([]ai.OutcomeObjectContext, 0, len(s.objects))
	seen := map[string]struct{}{}
	for _, obj := range s.objects {
		if obj == nil {
			continue
		}
		objLayer, _ := obj.State["layer"].(string)
		objLayer = strings.TrimSpace(objLayer)
		if objLayer != layerID {
			continue
		}
		if _, ok := seen[obj.ID]; ok {
			continue
		}
		seen[obj.ID] = struct{}{}
		out = append(out, ai.OutcomeObjectContext{
			ID:          obj.ID,
			Name:        obj.Name,
			Tags:        append([]string{}, obj.Tags...),
			Layer:       objLayer,
			State:       cloneObjectState(obj.State),
			Description: extractObjectDescription(obj.State),
		})
	}
	return out
}

func (s *SceneInstance) spawnItemsFromOutcome(req ai.OutcomeRequest, outcome ai.OutcomeResult, player *model.PlayerState, source *model.GameObject, itemLayerHints map[string]string) ([]map[string]any, error) {
	itemNames := outcome.Items
	if req.Interaction == "open_container" || req.Interaction == "interact" {
		// For container interaction, only accept explicit outcome objects.
		if refined, refineErr := ai.RefineObtainedItemsWithAI(req, outcome.Narration, itemNames); refineErr == nil {
			itemNames = refined
		}
		if len(itemNames) == 0 {
			if extracted, extractErr := ai.ExtractItemsFromNarrationWithAI(outcome.Narration); extractErr == nil {
				if refined, refineErr := ai.RefineObtainedItemsWithAI(req, outcome.Narration, extracted); refineErr == nil && len(refined) > 0 {
					itemNames = refined
				} else {
					itemNames = extracted
				}
			}
		}
		itemNames = normalizeOutcomeItemNames(itemNames, req)
		return s.spawnOutcomeItemsInLayers(req, outcome, player, source, itemNames, itemLayerHints)
	}
	if refined, refineErr := ai.RefineObtainedItemsWithAI(req, outcome.Narration, itemNames); refineErr == nil && len(refined) > 0 {
		itemNames = refined
	}
	// For scene bootstrap, keep obtained items and merge scene-level interactables.
	if req.Interaction == "scene_bootstrap" {
		if scanned, scanErr := ai.ExtractInteractiveItemsFromNarrationWithAI(req, outcome.Narration); scanErr == nil && len(scanned) > 0 {
			itemNames = append(itemNames, scanned...)
		}
	}
	if len(itemNames) == 0 {
		if extracted, extractErr := ai.ExtractItemsFromNarrationWithAI(outcome.Narration); extractErr == nil && len(extracted) > 0 {
			itemNames = extracted
		}
	}
	if len(itemNames) == 0 {
		itemNames = ai.ExtractItemNamesFromNarration(outcome.Narration)
	}
	if len(itemNames) == 0 {
		if generated, genErr := ai.GenerateLootItemsWithAI(req, outcome.Narration); genErr == nil && len(generated) > 0 {
			itemNames = generated
		}
	}
	itemNames = normalizeOutcomeItemNames(itemNames, req)
	return s.spawnOutcomeItemsInLayers(req, outcome, player, source, itemNames, itemLayerHints)
}

func (s *SceneInstance) spawnOutcomeItemsInLayers(req ai.OutcomeRequest, outcome ai.OutcomeResult, player *model.PlayerState, source *model.GameObject, itemNames []string, itemLayerHints map[string]string) ([]map[string]any, error) {
	if len(itemNames) == 0 {
		return []map[string]any{}, nil
	}

	spawned := make([]map[string]any, 0, len(itemNames))
	defaultLayer := s.ID
	if player != nil && len(player.LayerStack) > 0 {
		defaultLayer = player.LayerStack[len(player.LayerStack)-1]
	}
	for _, name := range itemNames {
		targetLayer := defaultLayer
		if itemLayerHints != nil {
			if hintedLayer := strings.TrimSpace(resolveHintedLayerForItem(name, itemLayerHints)); hintedLayer != "" {
				targetLayer = hintedLayer
				if _, ok := s.knownLayers[targetLayer]; !ok {
					s.RegisterLayer(targetLayer)
					s.applyLayerRelation(defaultLayer, targetLayer, "child")
				}
			}
		}
		if existing := s.findExistingItemByName(name, targetLayer); existing != nil {
			if existing.State == nil {
				existing.State = map[string]any{}
			}
			if strings.TrimSpace(extractObjectDescription(existing.State)) == "" {
				existing.State["description"] = buildOutcomeItemDescription(name, targetLayer, req.Interaction, outcome.Narration)
			}
			spawned = append(spawned, map[string]any{
				"id":          existing.ID,
				"name":        existing.Name,
				"layer":       targetLayer,
				"description": extractObjectDescription(existing.State),
				"reused":      true,
			})
			continue
		}
		spec := &model.CreateObjectSpec{
			Name: name,
			Tags: []string{"item", "outcome"},
			State: map[string]any{
				"from_interaction": req.Interaction,
				"layer":            targetLayer,
				"description":      buildOutcomeItemDescription(name, targetLayer, req.Interaction, outcome.Narration),
			},
		}
		if source != nil {
			spec.State["from_object_id"] = source.ID
		}
		created := s.createObjectFromAction(spec, player)
		if _, exists := s.objects[created.ID]; exists {
			created.ID = fmt.Sprintf("%s-%d", created.ID, s.state.Version+int64(len(spawned))+1)
		}
		s.AddObject(created)
		spawned = append(spawned, map[string]any{
			"id":          created.ID,
			"name":        created.Name,
			"layer":       targetLayer,
			"description": extractObjectDescription(created.State),
		})
	}
	if len(spawned) > 0 {
		s.state.Version++
	}
	return spawned, nil
}

func (s *SceneInstance) planOutcomeLayerPlacement(req ai.OutcomeRequest, narration, defaultLayer string) (map[string]string, []map[string]any) {
	defaultLayer = strings.TrimSpace(defaultLayer)
	if req.Interaction != "scene_bootstrap" || defaultLayer == "" || strings.TrimSpace(narration) == "" {
		return nil, nil
	}
	plan, err := ai.PlanOutcomeLayersAndItemsWithAI(req, narration, defaultLayer)
	if err != nil {
		return nil, nil
	}
	hints := map[string]string{}
	created := make([]map[string]any, 0)
	for _, layer := range plan.Layers {
		layerID := strings.TrimSpace(layer.LayerID)
		if layerID == "" || layerID == defaultLayer {
			continue
		}
		if !isLikelySpatialLayerName(layerID) {
			continue
		}
		parentLayer := strings.TrimSpace(layer.ParentLayer)
		if parentLayer == "" {
			parentLayer = defaultLayer
		}
		if _, ok := s.knownLayers[parentLayer]; !ok {
			s.RegisterLayer(parentLayer)
		}
		if _, exists := s.knownLayers[layerID]; !exists {
			s.RegisterLayer(layerID)
			relation := normalizeLayerRelation(layer.Relation)
			s.applyLayerRelation(parentLayer, layerID, relation)
			created = append(created, map[string]any{
				"layer_id":     layerID,
				"parent_layer": parentLayer,
				"relation":     relation,
			})
		}
		if strings.TrimSpace(layer.Description) != "" {
			s.SetLayerDescription(layerID, layer.Description)
		}
	}
	for _, item := range plan.Items {
		itemName := normalizeName(item.ItemName)
		layerID := strings.TrimSpace(item.LayerID)
		if itemName == "" || layerID == "" {
			continue
		}
		if _, ok := s.knownLayers[layerID]; !ok {
			if !isLikelySpatialLayerName(layerID) {
				continue
			}
			s.RegisterLayer(layerID)
			s.applyLayerRelation(defaultLayer, layerID, "child")
			created = append(created, map[string]any{
				"layer_id":     layerID,
				"parent_layer": defaultLayer,
				"relation":     "child",
			})
		}
		hints[itemName] = layerID
	}
	if len(hints) == 0 {
		hints = nil
	}
	return hints, created
}

func normalizeLayerRelation(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "child", "sibling", "parent", "same":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "child"
	}
}

func isLikelySpatialLayerName(name string) bool {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" {
		return false
	}
	spaceKeywords := []string{
		"房", "厅", "室", "间", "走廊", "廊", "楼层", "楼道", "楼梯口", "玄关", "门厅", "庭院", "地下室", "阁楼", "客厅", "卧室", "书房", "厨房", "浴室", "仓库", "大厅",
		"room", "hall", "corridor", "floor", "basement", "attic", "lobby", "kitchen", "bedroom", "study", "bathroom", "warehouse", "stairwell",
	}
	objectKeywords := []string{
		"柜", "书柜", "桌", "椅", "灯", "吊灯", "烛台", "壁炉", "镜", "镜子", "钟", "沙发", "床", "门", "窗", "杯", "壶", "棍", "画", "信", "白布",
		"cabinet", "shelf", "table", "chair", "lamp", "chandelier", "fireplace", "mirror", "clock", "sofa", "bed", "door", "window", "cup", "rod", "painting", "letter",
	}
	for _, bad := range objectKeywords {
		if strings.Contains(n, bad) {
			return false
		}
	}
	for _, kw := range spaceKeywords {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

func resolveHintedLayerForItem(item string, hints map[string]string) string {
	if len(hints) == 0 {
		return ""
	}
	key := normalizeName(item)
	if v := strings.TrimSpace(hints[key]); v != "" {
		return v
	}
	for hintItem, layerID := range hints {
		if hintItem == "" || layerID == "" {
			continue
		}
		if strings.Contains(key, hintItem) || strings.Contains(hintItem, key) {
			return layerID
		}
	}
	return ""
}

func normalizeOutcomeItemNames(items []string, req ai.OutcomeRequest) []string {
	if len(items) == 0 {
		return nil
	}
	if normalized, err := ai.NormalizeItemsToChineseWithAI(req, items); err == nil && len(normalized) > 0 {
		items = normalized
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = normalizeItemByDictionary(item)
		if !containsChineseRune(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeItemByDictionary(item string) string {
	lower := strings.ToLower(strings.TrimSpace(item))
	dict := map[string]string{
		"goblin ear":         "鍝ュ竷鏋楄€虫湹",
		"rusty iron key":     "閿堥搧閽ュ寵",
		"dragon scale shard": "榫欓碁纰庣墖",
		"bronze oil lamp":    "榛勯摐娌圭伅",
		"oil lamp":           "娌圭伅",
		"parchment scroll":   "缇婄毊鍗疯酱",
		"parchment":          "羊皮纸",
		"old key":            "旧钥匙",
	}
	if mapped, ok := dict[lower]; ok {
		return mapped
	}
	return item
}

func containsChineseRune(v string) bool {
	for _, r := range v {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func (s *SceneInstance) collectKnownItemsInLayer(layerID string) []ai.OutcomeObjectContext {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return nil
	}
	out := make([]ai.OutcomeObjectContext, 0)
	seen := map[string]struct{}{}
	for _, obj := range s.objects {
		if obj == nil || !isItemObject(obj) {
			continue
		}
		objLayer, _ := obj.State["layer"].(string)
		objLayer = strings.TrimSpace(objLayer)
		if objLayer != layerID {
			continue
		}
		if _, ok := seen[obj.ID]; ok {
			continue
		}
		seen[obj.ID] = struct{}{}
		out = append(out, ai.OutcomeObjectContext{
			ID:          obj.ID,
			Name:        obj.Name,
			Tags:        append([]string{}, obj.Tags...),
			Layer:       objLayer,
			State:       cloneObjectState(obj.State),
			Description: extractObjectDescription(obj.State),
		})
	}
	// Merge persisted snapshot for this specific layer.
	if s.state.Meta != nil {
		if raw, ok := s.state.Meta[layerItemStatesMetaKey].(map[string]any); ok {
			if values, ok := raw[layerID]; ok {
				items, ok := values.([]any)
				if ok {
					for _, item := range items {
						entry, ok := item.(map[string]any)
						if !ok {
							continue
						}
						id, _ := entry["id"].(string)
						name, _ := entry["name"].(string)
						if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
							continue
						}
						if _, ok := seen[id]; ok {
							continue
						}
						seen[id] = struct{}{}
						out = append(out, ai.OutcomeObjectContext{
							ID:          id,
							Name:        name,
							Layer:       layerID,
							State:       cloneObjectState(asAnyMap(entry["state"])),
							Description: firstNonEmptyString(entry["description"], entry["desc"]),
						})
					}
				}
			}
		}
	}
	return out
}

func (s *SceneInstance) previousLayerForPlayer(playerID, currentLayer string) string {
	currentLayer = strings.TrimSpace(currentLayer)
	player := s.players[playerID]
	if player == nil || len(player.LayerStack) == 0 {
		return s.ID
	}
	for i := len(player.LayerStack) - 1; i >= 0; i-- {
		layerID := strings.TrimSpace(player.LayerStack[i])
		if layerID == "" {
			continue
		}
		if currentLayer != "" && layerID == currentLayer {
			continue
		}
		return layerID
	}
	return s.ID
}

func isItemObject(obj *model.GameObject) bool {
	for _, tag := range obj.Tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "item", "loot", "outcome":
			return true
		}
	}
	return false
}

func (s *SceneInstance) findExistingItemByName(name, layerID string) *model.GameObject {
	normalized := normalizeName(name)
	if normalized == "" {
		return nil
	}
	for _, obj := range s.objects {
		if obj == nil || !isItemObject(obj) {
			continue
		}
		if normalizeName(obj.Name) != normalized {
			continue
		}
		objLayer, _ := obj.State["layer"].(string)
		if strings.TrimSpace(objLayer) == strings.TrimSpace(layerID) {
			return obj
		}
	}
	return nil
}

func cloneObjectState(state map[string]any) map[string]any {
	if len(state) == 0 {
		return nil
	}
	out := make(map[string]any, len(state))
	for k, v := range state {
		out[k] = v
	}
	return out
}

func extractObjectDescription(state map[string]any) string {
	if len(state) == 0 {
		return ""
	}
	return firstNonEmptyString(state["description"], state["desc"], state["status"], state["state_desc"])
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func asAnyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func normalizeName(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, "-", " ")
	return strings.Join(strings.Fields(v), " ")
}

func buildOutcomeItemDescription(name, layerID, interaction, narration string) string {
	name = strings.TrimSpace(name)
	layerID = strings.TrimSpace(layerID)
	interaction = strings.TrimSpace(interaction)
	if interaction == "open_container" || interaction == "interact" || interaction == "observe" {
		return deriveSpawnedItemDescription(narration, name)
	}
	if sentence := findNarrationSentenceForItem(narration, name); sentence != "" {
		return normalizeStateSentence(sentence)
	}
	if layerID != "" && interaction != "" {
		return fmt.Sprintf("Located in %s; from %s.", layerID, interaction)
	}
	if layerID != "" {
		return fmt.Sprintf("Located in %s.", layerID)
	}
	return fmt.Sprintf("Interactive item: %s.", name)
}

func findNarrationSentenceForItem(narration, itemName string) string {
	narration = strings.TrimSpace(narration)
	itemName = strings.TrimSpace(itemName)
	if narration == "" || itemName == "" {
		return ""
	}
	parts := strings.FieldsFunc(narration, func(r rune) bool {
		switch r {
		case '。', '.', '！', '!', '？', '?', '\n':
			return true
		default:
			return false
		}
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, itemName) {
			return part
		}
	}
	return ""
}
