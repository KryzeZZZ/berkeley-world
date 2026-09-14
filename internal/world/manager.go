package world

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"SAO/internal/model"
	"SAO/internal/repo"
	"SAO/internal/scene"
)

type Manager struct {
	mu                 sync.RWMutex
	scenes             map[string]*scene.SceneInstance
	playerToScene      map[string]string
	objectRepo         repo.ObjectRepository
	persistPath        string
	allowSceneCreation bool
	eventSubscribers   map[string]map[uint64]chan model.Event
	nextSubscriberID   uint64
}

func NewManager(objectRepo repo.ObjectRepository) *Manager {
	if objectRepo == nil {
		objectRepo = repo.NewInMemoryObjectRepository()
	}
	return &Manager{
		scenes:             map[string]*scene.SceneInstance{},
		playerToScene:      map[string]string{},
		objectRepo:         objectRepo,
		allowSceneCreation: true,
		eventSubscribers:   map[string]map[uint64]chan model.Event{},
	}
}

func (m *Manager) AddScene(instance *scene.SceneInstance) {
	m.mu.Lock()
	instance.BindObjectRepository(m.objectRepo)
	instance.SetAllowLayerCreation(m.allowSceneCreation)
	m.scenes[instance.ID] = instance
	m.mu.Unlock()
	go instance.Run()
	go func(inst *scene.SceneInstance) {
		for event := range inst.Events() {
			m.handleSceneEvent(event)
		}
	}(instance)
}

func (m *Manager) handleSceneEvent(event model.Event) {
	_ = m.persistIfEnabled()

	m.mu.RLock()
	subs := m.eventSubscribers[event.PlayerID]
	targets := make([]chan model.Event, 0, len(subs))
	for _, ch := range subs {
		targets = append(targets, ch)
	}
	m.mu.RUnlock()

	for _, ch := range targets {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) SubscribePlayerEvents(playerID string, buffer int) (uint64, <-chan model.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.playerToScene[playerID]; !ok {
		return 0, nil, fmt.Errorf("player %s has no active scene", playerID)
	}
	if buffer <= 0 {
		buffer = 16
	}
	id := atomic.AddUint64(&m.nextSubscriberID, 1)
	ch := make(chan model.Event, buffer)
	if m.eventSubscribers[playerID] == nil {
		m.eventSubscribers[playerID] = map[uint64]chan model.Event{}
	}
	m.eventSubscribers[playerID][id] = ch
	return id, ch, nil
}

func (m *Manager) UnsubscribePlayerEvents(playerID string, subscriberID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	subs := m.eventSubscribers[playerID]
	if subs == nil {
		return
	}
	ch, ok := subs[subscriberID]
	if !ok {
		return
	}
	delete(subs, subscriberID)
	if len(subs) == 0 {
		delete(m.eventSubscribers, playerID)
	}
	close(ch)
}

func (m *Manager) SetAllowSceneCreation(allow bool) {
	m.mu.Lock()
	m.allowSceneCreation = allow
	for _, instance := range m.scenes {
		instance.SetAllowLayerCreation(allow)
	}
	m.mu.Unlock()
}

func (m *Manager) AddPlayer(sceneID string, player *model.PlayerState) error {
	m.mu.Lock()
	instance, ok := m.scenes[sceneID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("scene %s not found", sceneID)
	}
	instance.AddPlayer(player)
	m.playerToScene[player.ID] = sceneID
	m.mu.Unlock()
	_ = m.persistIfEnabled()
	return nil
}

func (m *Manager) AddObject(sceneID string, obj *model.GameObject) error {
	m.mu.RLock()
	instance, ok := m.scenes[sceneID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("scene %s not found", sceneID)
	}
	instance.AddObject(obj)
	if err := m.objectRepo.SaveObject(sceneID, obj); err != nil {
		return err
	}
	_ = m.persistIfEnabled()
	return nil
}

func (m *Manager) EnqueueAction(envelope model.ActionEnvelope) error {
	m.mu.RLock()
	sceneID, ok := m.playerToScene[envelope.PlayerID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("player %s has no active scene", envelope.PlayerID)
	}
	instance := m.scenes[sceneID]
	m.mu.RUnlock()
	if instance == nil {
		return fmt.Errorf("scene %s not found", sceneID)
	}
	if envelope.Action.Type == "push_layer" || envelope.Action.Type == "replace_layer" {
		resolvedLayerID, err := instance.ResolveLayerID(envelope.PlayerID, envelope.Action.LayerID)
		if err != nil {
			return err
		}
		envelope.Action.LayerID = resolvedLayerID
	}
	if envelope.Action.Type == "interact" && envelope.Action.TargetObjectID == "" && envelope.Action.TargetQuery != "" {
		targetID, err := m.ResolveTargetObjectID(envelope.PlayerID, envelope.Action.TargetQuery)
		if err != nil {
			return err
		}
		envelope.Action.TargetObjectID = targetID
	}
	if envelope.Action.Type == "observe" && envelope.Action.TargetObjectID == "" && envelope.Action.TargetQuery != "" {
		if isCurrentSurroundingsQuery(envelope.Action.TargetQuery) {
			envelope.Action.TargetQuery = ""
		} else if targetID, err := m.ResolveTargetObjectID(envelope.PlayerID, envelope.Action.TargetQuery); err == nil {
			envelope.Action.TargetObjectID = targetID
		} else {
			layerID, layerErr := instance.ResolveLayerID(envelope.PlayerID, envelope.Action.TargetQuery)
			if layerErr != nil {
				return err
			}
			envelope.Action.LayerID = layerID
		}
	}
	return instance.Enqueue(model.QueuedAction{
		PlayerID: envelope.PlayerID,
		Action:   envelope.Action,
	})
}

func isCurrentSurroundingsQuery(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	normalized = strings.Trim(normalized, "，。！？!?., ")
	switch normalized {
	case "四周", "周围", "附近", "这里", "此处", "当前区域", "当前地点",
		"surroundings", "around", "nearby", "here", "current area", "current location":
		return true
	default:
		return false
	}
}

func (m *Manager) ResolveTargetObjectID(playerID, query string) (string, error) {
	m.mu.RLock()
	sceneID, ok := m.playerToScene[playerID]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("player %s has no active scene", playerID)
	}
	instance := m.scenes[sceneID]
	m.mu.RUnlock()
	if instance == nil {
		return "", fmt.Errorf("scene %s not found", sceneID)
	}
	obj, err := instance.ResolveVisibleObjectByNameOrTag(playerID, query)
	if err != nil {
		return "", err
	}
	return obj.ID, nil
}

func (m *Manager) GetSceneByPlayer(playerID string) (*scene.SceneInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sceneID, ok := m.playerToScene[playerID]
	if !ok {
		return nil, fmt.Errorf("player %s has no active scene", playerID)
	}
	instance := m.scenes[sceneID]
	if instance == nil {
		return nil, fmt.Errorf("scene %s not found", sceneID)
	}
	return instance, nil
}

func (m *Manager) CreateLayerForPlayer(playerID, layerID, relation, parentLayerID string) (string, error) {
	m.mu.RLock()
	sceneID, ok := m.playerToScene[playerID]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("player %s has no active scene", playerID)
	}
	instance := m.scenes[sceneID]
	m.mu.RUnlock()
	if instance == nil {
		return "", fmt.Errorf("scene %s not found", sceneID)
	}
	if !instance.AllowLayerCreation() {
		return "", fmt.Errorf("scene creation is disabled by policy")
	}
	createdID, err := instance.CreateLayerForPlayer(playerID, layerID, relation, parentLayerID)
	if err != nil {
		return "", err
	}
	_ = m.persistIfEnabled()
	return createdID, nil
}

func (m *Manager) ListPlayers() []*model.PlayerState {
	m.mu.RLock()
	sceneIDs := make([]string, 0, len(m.scenes))
	for sceneID := range m.scenes {
		sceneIDs = append(sceneIDs, sceneID)
	}
	sort.Strings(sceneIDs)
	scenes := make([]*scene.SceneInstance, 0, len(sceneIDs))
	for _, sceneID := range sceneIDs {
		scenes = append(scenes, m.scenes[sceneID])
	}
	m.mu.RUnlock()

	out := make([]*model.PlayerState, 0)
	for _, inst := range scenes {
		if inst == nil {
			continue
		}
		out = append(out, inst.ListPlayerInfos()...)
	}
	return out
}
