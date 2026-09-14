package scene

import (
	"fmt"
	"sort"
	"strings"

	"SAO/internal/dice"
	"SAO/internal/model"
	"SAO/internal/repo"
)

type SceneInstance struct {
	ID                 string
	actionQueue        chan model.QueuedAction
	queueSem           chan struct{}
	snapshotReq        chan chan SceneSnapshot
	descriptionQueue   chan descriptionTask
	descriptionPending map[string]struct{}
	state              model.SceneState
	layerStack         *LayerStack
	knownLayers        map[string]struct{}
	allowLayerCreation bool
	objects            map[string]*model.GameObject
	players            map[string]*model.PlayerState
	eventStream        chan model.Event
	roller             dice.Roller
	objectRepo         repo.ObjectRepository
}

func NewSceneInstance(sceneID string, queueSize int, roller dice.Roller) *SceneInstance {
	if queueSize <= 0 {
		queueSize = 64
	}
	if roller == nil {
		roller = dice.NewDefaultRoller()
	}
	instance := &SceneInstance{
		ID:               sceneID,
		actionQueue:      make(chan model.QueuedAction, queueSize),
		queueSem:         make(chan struct{}, 1),
		snapshotReq:      make(chan chan SceneSnapshot),
		descriptionQueue: make(chan descriptionTask, queueSize),
		state: model.SceneState{
			ID:      sceneID,
			Version: 1,
			Meta:    map[string]any{},
		},
		layerStack:         NewLayerStack(sceneID),
		knownLayers:        map[string]struct{}{sceneID: {}},
		allowLayerCreation: true,
		objects:            map[string]*model.GameObject{},
		players:            map[string]*model.PlayerState{},
		eventStream:        make(chan model.Event, 128),
		roller:             roller,
	}
	instance.MarkLayerBootstrapped(sceneID)
	return instance
}

func (s *SceneInstance) BindObjectRepository(objectRepo repo.ObjectRepository) {
	s.objectRepo = objectRepo
}

func (s *SceneInstance) Run() {
	go s.runDescriptionWorker()
	for {
		select {
		case queued, ok := <-s.actionQueue:
			if !ok {
				return
			}
			s.processActionSync(queued)
		case ch := <-s.snapshotReq:
			ch <- s.snapshotUnsafe()
		}
	}
}

func (s *SceneInstance) Enqueue(action model.QueuedAction) error {
	select {
	case s.actionQueue <- action:
		return nil
	default:
		return fmt.Errorf("scene %s action queue is full", s.ID)
	}
}

func (s *SceneInstance) processActionSync(queued model.QueuedAction) {
	s.queueSem <- struct{}{}
	defer func() { <-s.queueSem }()
	s.processAction(queued)
}

func (s *SceneInstance) AddPlayer(player *model.PlayerState) {
	if player == nil {
		return
	}
	if len(player.LayerStack) == 0 {
		player.LayerStack = []string{s.ID}
	}
	for _, layerID := range player.LayerStack {
		s.RegisterLayer(layerID)
	}
	if player.InventoryObjIDs == nil {
		player.InventoryObjIDs = []string{}
	}
	player.Attributes.Normalize()
	player.SceneID = s.ID
	s.players[player.ID] = player
}

func (s *SceneInstance) AddObject(obj *model.GameObject) {
	if obj == nil {
		return
	}
	if obj.State == nil {
		obj.State = map[string]any{}
	}
	ensureObjectStateDescription(obj)
	if obj.Version == 0 {
		obj.Version = 1
	}
	if _, ok := obj.State["layer"]; !ok {
		obj.State["layer"] = s.ID
	}
	if layerID, _ := obj.State["layer"].(string); strings.TrimSpace(layerID) != "" {
		s.RegisterLayer(layerID)
	}
	s.objects[obj.ID] = obj
	if s.objectRepo != nil {
		_ = s.objectRepo.SaveObject(s.ID, obj)
	}
}

func (s *SceneInstance) RegisterLayer(layerID string) {
	layerID = strings.TrimSpace(layerID)
	if layerID == "" {
		return
	}
	if s.knownLayers == nil {
		s.knownLayers = map[string]struct{}{}
	}
	s.knownLayers[layerID] = struct{}{}
}

func (s *SceneInstance) SetAllowLayerCreation(allow bool) {
	s.allowLayerCreation = allow
}

func (s *SceneInstance) AllowLayerCreation() bool {
	return s.allowLayerCreation
}

func (s *SceneInstance) PushLayer(sceneID string) {
	s.layerStack.PushLayer(sceneID)
}

func (s *SceneInstance) PopLayer() string {
	return s.layerStack.PopLayer()
}

func (s *SceneInstance) ReplaceLayer(sceneID string) {
	s.layerStack.ReplaceLayer(sceneID)
}

func (s *SceneInstance) PushLayerForPlayer(playerID, sceneID string) error {
	player, ok := s.players[playerID]
	if !ok {
		return fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	resolvedLayerID, err := s.ResolveLayerID(playerID, sceneID)
	if err != nil {
		return err
	}
	if canonical := s.canonicalStackToLayer(resolvedLayerID); len(canonical) > 0 {
		if !sameStringSlice(player.LayerStack, canonical) {
			player.LayerStack = canonical
		}
		return nil
	}
	s.RegisterLayer(resolvedLayerID)
	stack := &LayerStack{layers: append([]string{}, player.LayerStack...)}
	stack.PushLayer(resolvedLayerID)
	player.LayerStack = stack.Snapshot()
	return nil
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *SceneInstance) canonicalStackToLayer(targetLayer string) []string {
	targetLayer = strings.TrimSpace(targetLayer)
	if targetLayer == "" {
		return nil
	}
	chain := make([]string, 0, 8)
	visited := map[string]struct{}{}
	curr := targetLayer
	for curr != "" {
		if _, seen := visited[curr]; seen {
			break
		}
		visited[curr] = struct{}{}
		chain = append(chain, curr)
		if curr == s.ID {
			break
		}
		curr = strings.TrimSpace(s.getLayerParent(curr))
	}
	if len(chain) == 0 {
		return nil
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	if chain[0] != s.ID {
		chain = append([]string{s.ID}, chain...)
	}
	out := make([]string, 0, len(chain))
	seen := map[string]struct{}{}
	for _, layerID := range chain {
		layerID = strings.TrimSpace(layerID)
		if layerID == "" {
			continue
		}
		if _, ok := seen[layerID]; ok {
			continue
		}
		seen[layerID] = struct{}{}
		s.RegisterLayer(layerID)
		out = append(out, layerID)
	}
	return out
}

func (s *SceneInstance) PopLayerForPlayer(playerID string) (string, error) {
	player, ok := s.players[playerID]
	if !ok {
		return "", fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	stack := &LayerStack{layers: append([]string{}, player.LayerStack...)}
	popped := stack.PopLayer()
	if len(stack.layers) == 0 {
		stack.PushLayer(s.ID)
	}
	player.LayerStack = stack.Snapshot()
	return popped, nil
}

func (s *SceneInstance) ReplaceLayerForPlayer(playerID, sceneID string) error {
	player, ok := s.players[playerID]
	if !ok {
		return fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	resolvedLayerID, err := s.ResolveLayerID(playerID, sceneID)
	if err != nil {
		return err
	}
	s.RegisterLayer(resolvedLayerID)
	stack := &LayerStack{layers: append([]string{}, player.LayerStack...)}
	stack.ReplaceLayer(resolvedLayerID)
	player.LayerStack = stack.Snapshot()
	return nil
}

func (s *SceneInstance) GetPlayerLayerStack(playerID string) ([]string, error) {
	player, ok := s.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	out := make([]string, len(player.LayerStack))
	copy(out, player.LayerStack)
	return out, nil
}

func (s *SceneInstance) GetVisibleObjects(playerID string) []*model.GameObject {
	player, ok := s.players[playerID]
	if !ok {
		return nil
	}
	currentLayer := s.ID
	if len(player.LayerStack) > 0 {
		currentLayer = strings.TrimSpace(player.LayerStack[len(player.LayerStack)-1])
	}
	if currentLayer == "" {
		currentLayer = s.ID
	}
	return GetVisibleObjects([]string{currentLayer}, s.objects)
}

func (s *SceneInstance) Events() <-chan model.Event {
	return s.eventStream
}

func (s *SceneInstance) Snapshot() SceneSnapshot {
	resp := make(chan SceneSnapshot, 1)
	s.snapshotReq <- resp
	return <-resp
}

func (s *SceneInstance) GetPlayerInventory(playerID string) ([]*model.GameObject, error) {
	player, ok := s.players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	out := make([]*model.GameObject, 0, len(player.InventoryObjIDs))
	for _, objID := range player.InventoryObjIDs {
		if obj := s.objects[objID]; obj != nil {
			out = append(out, obj)
		}
	}
	return out, nil
}

func (s *SceneInstance) GetPlayerInfo(playerID string) (*model.PlayerState, error) {
	player, ok := s.players[playerID]
	if !ok || player == nil {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	cloned := *player
	if len(player.LayerStack) > 0 {
		cloned.LayerStack = append([]string{}, player.LayerStack...)
	}
	if len(player.InventoryObjIDs) > 0 {
		cloned.InventoryObjIDs = append([]string{}, player.InventoryObjIDs...)
	}
	cloned.Attributes.Normalize()
	return &cloned, nil
}

func (s *SceneInstance) LockPlayerToDevice(playerID, deviceID string) (*model.PlayerState, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("device_id is required")
	}
	player, ok := s.players[playerID]
	if !ok || player == nil {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	if player.DeviceLocked && player.DeviceID != "" && player.DeviceID != deviceID {
		return nil, fmt.Errorf("player %s is locked to another device", playerID)
	}
	player.DeviceLocked = true
	player.DeviceID = deviceID
	return s.GetPlayerInfo(playerID)
}

func (s *SceneInstance) UnlockPlayerDevice(playerID, deviceID string) (*model.PlayerState, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("device_id is required")
	}
	player, ok := s.players[playerID]
	if !ok || player == nil {
		return nil, fmt.Errorf("player %s not found in scene %s", playerID, s.ID)
	}
	if strings.TrimSpace(player.DeviceID) != deviceID {
		return nil, fmt.Errorf("device_id does not match current login device")
	}
	player.DeviceID = ""
	player.DeviceLocked = false
	return s.GetPlayerInfo(playerID)
}

func (s *SceneInstance) ListPlayerInfos() []*model.PlayerState {
	ids := make([]string, 0, len(s.players))
	for id := range s.players {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*model.PlayerState, 0, len(ids))
	for _, id := range ids {
		player, err := s.GetPlayerInfo(id)
		if err == nil && player != nil {
			out = append(out, player)
		}
	}
	return out
}
