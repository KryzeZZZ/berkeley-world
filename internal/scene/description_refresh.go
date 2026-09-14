package scene

import (
	"fmt"
	"strings"

	"SAO/internal/ai"
	"SAO/internal/model"
)

type descriptionTaskKind int

const (
	descriptionTaskObject descriptionTaskKind = iota
	descriptionTaskLayer
)

type descriptionTask struct {
	kind descriptionTaskKind
	id   string
}

func (s *SceneInstance) ScheduleObjectDescriptionRefresh(objectID string) {
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return
	}
	s.scheduleDescriptionTask(descriptionTask{kind: descriptionTaskObject, id: objectID})
}

func (s *SceneInstance) ScheduleLayerDescriptionRefresh(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	s.scheduleDescriptionTask(descriptionTask{kind: descriptionTaskLayer, id: layerID})
}

func (s *SceneInstance) scheduleDescriptionTask(task descriptionTask) {
	if s == nil {
		return
	}
	key := descriptionTaskKey(task)
	if key == "" {
		return
	}
	s.queueSem <- struct{}{}
	s.scheduleDescriptionTaskLocked(task, key)
	<-s.queueSem
}

func (s *SceneInstance) scheduleDescriptionTaskLocked(task descriptionTask, key string) {
	if s.descriptionPending == nil {
		s.descriptionPending = map[string]struct{}{}
	}
	if _, exists := s.descriptionPending[key]; exists {
		return
	}
	s.descriptionPending[key] = struct{}{}

	select {
	case s.descriptionQueue <- task:
	default:
		delete(s.descriptionPending, key)
	}
}

func (s *SceneInstance) scheduleObjectDescriptionRefreshLocked(objectID string) {
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return
	}
	task := descriptionTask{kind: descriptionTaskObject, id: objectID}
	key := descriptionTaskKey(task)
	if key == "" {
		return
	}
	s.scheduleDescriptionTaskLocked(task, key)
}

func (s *SceneInstance) scheduleLayerDescriptionRefreshLocked(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	task := descriptionTask{kind: descriptionTaskLayer, id: layerID}
	key := descriptionTaskKey(task)
	if key == "" {
		return
	}
	s.SetLayerDescriptionStale(layerID, true)
	s.scheduleDescriptionTaskLocked(task, key)
}

func markObjectDescriptionStale(obj *model.GameObject) {
	if obj == nil {
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	obj.State["description_stale"] = true
}

func (s *SceneInstance) runDescriptionWorker() {
	for task := range s.descriptionQueue {
		s.processDescriptionTask(task)
	}
}

func (s *SceneInstance) processDescriptionTask(task descriptionTask) {
	key := descriptionTaskKey(task)
	if key == "" {
		return
	}
	switch task.kind {
	case descriptionTaskObject:
		s.refreshObjectDescription(task.id)
	case descriptionTaskLayer:
		s.refreshLayerDescription(task.id)
	}
	s.queueSem <- struct{}{}
	delete(s.descriptionPending, key)
	<-s.queueSem
}

func descriptionTaskKey(task descriptionTask) string {
	if strings.TrimSpace(task.id) == "" {
		return ""
	}
	switch task.kind {
	case descriptionTaskObject:
		return "obj:" + task.id
	case descriptionTaskLayer:
		return "layer:" + task.id
	default:
		return ""
	}
}

func (s *SceneInstance) refreshObjectDescription(objectID string) {
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return
	}
	var req ai.OutcomeRequest
	var objName string
	var objVersion int64
	var layerID string
	var snapshotState map[string]any

	s.queueSem <- struct{}{}
	obj := s.objects[objectID]
	if obj == nil {
		<-s.queueSem
		return
	}
	objName = obj.Name
	objVersion = obj.Version
	layerID, _ = obj.State["layer"].(string)
	snapshotState = cloneObjectState(obj.State)
	req = s.buildOutcomeRequestForObject(obj, layerID)
	<-s.queueSem

	req.Interaction = "observe"
	req.Instruction = "根据物品当前状态生成描述"
	req.Target = objName
	if req.Context.SceneDescription == "" {
		req.Context.SceneDescription = objName
	}
	req.Context.NearbyObjects = append([]ai.OutcomeObjectContext{
		{
			ID:          objectID,
			Name:        objName,
			Layer:       layerID,
			State:       snapshotState,
			Description: extractObjectDescription(snapshotState),
		},
	}, req.Context.NearbyObjects...)

	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		fallback := fallbackObjectNarration(objName)
		outcome = ai.OutcomeResult{Narration: fallback}
	}

	s.queueSem <- struct{}{}
	obj = s.objects[objectID]
	if obj == nil {
		<-s.queueSem
		return
	}
	if obj.Version != objVersion {
		// State changed after snapshot, keep stale flag and let future updates reschedule.
		<-s.queueSem
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	obj.State["description"] = deriveObjectStateDescription("观察", outcome.Narration, obj.Name)
	delete(obj.State, "description_stale")
	obj.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, obj)
	}
	<-s.queueSem
}

func (s *SceneInstance) RefreshObjectDescription(objectID string) (string, error) {
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return "", nil
	}
	var req ai.OutcomeRequest
	var objName string
	var layerID string
	var snapshotState map[string]any

	s.queueSem <- struct{}{}
	obj := s.objects[objectID]
	if obj == nil {
		<-s.queueSem
		return "", nil
	}
	objName = obj.Name
	layerID, _ = obj.State["layer"].(string)
	snapshotState = cloneObjectState(obj.State)
	req = s.buildOutcomeRequestForObject(obj, layerID)
	<-s.queueSem

	req.Interaction = "observe"
	req.Instruction = "根据物品当前状态生成描述"
	req.Target = objName
	if req.Context.SceneDescription == "" {
		req.Context.SceneDescription = objName
	}
	req.Context.NearbyObjects = append([]ai.OutcomeObjectContext{
		{
			ID:          objectID,
			Name:        objName,
			Layer:       layerID,
			State:       snapshotState,
			Description: extractObjectDescription(snapshotState),
		},
	}, req.Context.NearbyObjects...)

	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		fallback := fallbackObjectNarration(objName)
		outcome = ai.OutcomeResult{Narration: fallback}
	}

	s.queueSem <- struct{}{}
	obj = s.objects[objectID]
	if obj == nil {
		<-s.queueSem
		return "", nil
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	obj.State["description"] = deriveObjectStateDescription("观察", outcome.Narration, obj.Name)
	delete(obj.State, "description_stale")
	obj.Version++
	s.state.Version++
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, obj)
	}
	<-s.queueSem
	return outcome.Narration, nil
}

func (s *SceneInstance) refreshLayerDescription(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	var currentDesc string
	var objSnapshot []ai.OutcomeObjectContext
	var childLayers []string
	var knownItems []ai.OutcomeObjectContext

	s.queueSem <- struct{}{}
	currentDesc = s.GetLayerDescription(layerID)
	childLayers = s.collectChildLayers(layerID)
	objSnapshot = s.collectOutcomeObjectsInLayer(layerID)
	knownItems = s.collectKnownItemsInLayer(layerID)
	<-s.queueSem

	req := ai.OutcomeRequest{
		Interaction: "scene_refresh_no_loot",
		Instruction: "根据当前场景与物品状态生成描述",
		Target:      layerID,
		Context: ai.OutcomeContext{
			SceneID:          s.ID,
			CurrentLayer:     layerID,
			ChildLayers:      childLayers,
			SceneDescription: currentDesc,
			NearbyObjects:    objSnapshot,
			KnownItems:       knownItems,
		},
	}

	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		fallback := fallbackLayerNarration(layerID, currentDesc)
		outcome = ai.OutcomeResult{Narration: fallback}
	}

	s.queueSem <- struct{}{}
	s.SetLayerDescription(layerID, outcome.Narration)
	s.SetLayerDescriptionStale(layerID, false)
	s.state.Version++
	<-s.queueSem
}

func (s *SceneInstance) RefreshLayerDescription(layerID string) (string, error) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return "", nil
	}
	var currentDesc string
	var objSnapshot []ai.OutcomeObjectContext
	var childLayers []string
	var knownItems []ai.OutcomeObjectContext

	s.queueSem <- struct{}{}
	currentDesc = s.GetLayerDescription(layerID)
	childLayers = s.collectChildLayers(layerID)
	objSnapshot = s.collectOutcomeObjectsInLayer(layerID)
	knownItems = s.collectKnownItemsInLayer(layerID)
	<-s.queueSem

	req := ai.OutcomeRequest{
		Interaction: "scene_refresh_no_loot",
		Instruction: "根据当前场景与物品状态生成描述",
		Target:      layerID,
		Context: ai.OutcomeContext{
			SceneID:          s.ID,
			CurrentLayer:     layerID,
			ChildLayers:      childLayers,
			SceneDescription: currentDesc,
			NearbyObjects:    objSnapshot,
			KnownItems:       knownItems,
		},
	}
	outcome, err := ai.CallAIOutcomeNarrator(req)
	if err != nil {
		fallback := fallbackLayerNarration(layerID, currentDesc)
		outcome = ai.OutcomeResult{Narration: fallback}
	}

	s.queueSem <- struct{}{}
	s.SetLayerDescription(layerID, outcome.Narration)
	s.SetLayerDescriptionStale(layerID, false)
	s.state.Version++
	<-s.queueSem
	return outcome.Narration, nil
}

func (s *SceneInstance) buildOutcomeRequestForObject(obj *model.GameObject, layerID string) ai.OutcomeRequest {
	if obj == nil {
		return ai.OutcomeRequest{}
	}
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		layerID = s.ID
	}
	return ai.OutcomeRequest{
		Interaction: "observe",
		Instruction: "根据物品当前状态生成描述",
		Target:      obj.Name,
		Context: ai.OutcomeContext{
			SceneID:          s.ID,
			CurrentLayer:     layerID,
			ChildLayers:      s.collectChildLayers(layerID),
			SceneDescription: s.GetLayerDescription(layerID),
			NearbyObjects:    s.collectOutcomeObjectsInLayer(layerID),
			KnownItems:       s.collectKnownItemsInLayer(layerID),
		},
	}
}

func fallbackObjectNarration(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "你观察了物品。"
	}
	return fmt.Sprintf("你观察了%s。", name)
}

func fallbackLayerNarration(layerID, currentDesc string) string {
	currentDesc = strings.TrimSpace(currentDesc)
	if currentDesc != "" {
		return currentDesc
	}
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return "你观察了周围环境。"
	}
	return fmt.Sprintf("你观察了%s。", layerID)
}
