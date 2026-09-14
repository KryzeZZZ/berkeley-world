package pixelworld

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	MapWidth  = 30
	MapHeight = 21
)

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Player struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Inventory []Item `json:"inventory"`
	Position
}

type Item struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Quantity int            `json:"quantity"`
	State    map[string]any `json:"state"`
}

type Entity struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Position Position       `json:"position"`
	State    map[string]any `json:"state"`
}

type Event struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Detail    string     `json:"detail"`
	Player    string     `json:"player_id"`
	Revision  int        `json:"revision"`
	Time      int64      `json:"time"`
	Animation *Animation `json:"animation,omitempty"`
}

type Animation struct {
	Kind       string   `json:"kind"`
	Action     string   `json:"action"`
	TargetID   string   `json:"target_id,omitempty"`
	Position   Position `json:"position"`
	Frames     int      `json:"frames"`
	DurationMS int      `json:"duration_ms"`
}

type Snapshot struct {
	Revision     int               `json:"revision"`
	SceneID      string            `json:"scene_id"`
	Size         Position          `json:"size"`
	Players      map[string]Player `json:"players"`
	Entities     []Entity          `json:"entities"`
	ChangedTiles map[string]string `json:"changed_tiles"`
	EventLog     []Event           `json:"event_log"`
}

type Manager struct {
	mu          sync.RWMutex
	path        string
	snapshot    Snapshot
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

func NewManager(path string) (*Manager, error) {
	m := &Manager{path: path, subscribers: map[uint64]chan Event{}}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshot(m.snapshot)
}

func (m *Manager) Move(playerID string, destination Position) (Snapshot, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	player, ok := m.snapshot.Players[playerID]
	if !ok {
		return Snapshot{}, Event{}, fmt.Errorf("player %q not found", playerID)
	}
	if !isAdjacent(player.Position, destination) {
		return Snapshot{}, Event{}, errors.New("movement must target one adjacent tile")
	}
	if isBlocked(destination, m.snapshot) {
		return Snapshot{}, Event{}, errors.New("target tile is blocked")
	}
	player.Position = destination
	m.snapshot.Players[playerID] = player
	event := m.recordLocked("entity_moved", "位置更新", fmt.Sprintf("抵达 %d, %d", destination.X, destination.Y), playerID)
	if err := m.persistLocked(); err != nil {
		return Snapshot{}, Event{}, err
	}
	m.publishLocked(event)
	return cloneSnapshot(m.snapshot), event, nil
}

func (m *Manager) Interact(playerID, entityID string) (Snapshot, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	player, ok := m.snapshot.Players[playerID]
	if !ok {
		return Snapshot{}, Event{}, fmt.Errorf("player %q not found", playerID)
	}
	index := -1
	for i := range m.snapshot.Entities {
		if m.snapshot.Entities[i].ID == entityID {
			index = i
			break
		}
	}
	if index < 0 {
		return Snapshot{}, Event{}, fmt.Errorf("entity %q not found", entityID)
	}
	entity := &m.snapshot.Entities[index]
	if distance(player.Position, entity.Position) > 1 {
		return Snapshot{}, Event{}, errors.New("entity is not adjacent")
	}

	title, detail, eventType := applyInteraction(entity)
	event := m.recordLocked(eventType, title, detail, playerID)
	event.Animation = &Animation{Kind: entity.Kind, Action: "interact", TargetID: entity.ID, Position: entity.Position, Frames: 4, DurationMS: 420}
	if err := m.persistLocked(); err != nil {
		return Snapshot{}, Event{}, err
	}
	m.publishLocked(event)
	return cloneSnapshot(m.snapshot), event, nil
}

func (m *Manager) UseItem(playerID, itemID string, target Position) (Snapshot, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	player, ok := m.snapshot.Players[playerID]
	if !ok {
		return Snapshot{}, Event{}, fmt.Errorf("player %q not found", playerID)
	}
	if !isAdjacent(player.Position, target) {
		return Snapshot{}, Event{}, errors.New("item target must be adjacent")
	}
	if _, alreadyBroken := m.snapshot.ChangedTiles[tileKey(target)]; alreadyBroken {
		return Snapshot{}, Event{}, errors.New("target tile has already been destroyed")
	}
	item, ok := findItem(player.Inventory, itemID)
	if !ok {
		return Snapshot{}, Event{}, fmt.Errorf("item %q not found in inventory", itemID)
	}
	if item.Kind != "terrain_breaker" {
		return Snapshot{}, Event{}, errors.New("item cannot affect terrain")
	}
	for index := range m.snapshot.Entities {
		entity := &m.snapshot.Entities[index]
		if false && entity.Position == target && entity.State["destroyed"] != true {
			entity.State["destroyed"] = true
			event := m.recordLocked("entity_destroyed", "法杖破坏目标", entity.Name+" 已被破坏", playerID)
			event.Animation = &Animation{Kind: entity.Kind, Action: "break", TargetID: entity.ID, Position: target, Frames: 4, DurationMS: 420}
			if err := m.persistLocked(); err != nil {
				return Snapshot{}, Event{}, err
			}
			m.publishLocked(event)
			return cloneSnapshot(m.snapshot), event, nil
		}
	}
	destroyedCount := 0
	for index := range m.snapshot.Entities {
		entity := &m.snapshot.Entities[index]
		if entity.Position == target && entity.State["destroyed"] != true {
			entity.State["destroyed"] = true
			destroyedCount++
		}
	}
	if m.snapshot.ChangedTiles == nil {
		m.snapshot.ChangedTiles = map[string]string{}
	}
	originalTile := tileKind(target, m.snapshot)
	m.snapshot.ChangedTiles[tileKey(target)] = "rubble-" + originalTile
	event := m.recordLocked("tile_changed", "破界法杖生效", fmt.Sprintf("图格 %d, %d 已被击碎", target.X, target.Y), playerID)
	if destroyedCount > 0 {
		event.Detail = fmt.Sprintf("图格 %d, %d 及其 %d 个物品已被击碎", target.X, target.Y, destroyedCount)
	}
	event.Animation = &Animation{Kind: "effect-break-" + originalTile, Action: "break", Position: target, Frames: 12, DurationMS: 1050}
	if err := m.persistLocked(); err != nil {
		return Snapshot{}, Event{}, err
	}
	m.publishLocked(event)
	return cloneSnapshot(m.snapshot), event, nil
}

func (m *Manager) Reset() (Snapshot, Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = defaultSnapshot()
	m.ensureStarterKitLocked()
	event := m.recordLocked("world_reset", "区域已重置", "本地像素世界已恢复初始状态", "p1")
	if err := m.persistLocked(); err != nil {
		return Snapshot{}, Event{}, err
	}
	m.publishLocked(event)
	return cloneSnapshot(m.snapshot), event, nil
}

func (m *Manager) Subscribe() (uint64, <-chan Event, func()) {
	m.mu.Lock()
	id := m.nextSubID
	m.nextSubID++
	ch := make(chan Event, 16)
	m.subscribers[id] = ch
	m.mu.Unlock()
	return id, ch, func() {
		m.mu.Lock()
		if active, ok := m.subscribers[id]; ok {
			delete(m.subscribers, id)
			close(active)
		}
		m.mu.Unlock()
	}
}

// PublishRuntimeEvent broadcasts ephemeral renderer events. They are not
// persisted to the world file and therefore cannot affect gameplay state.
func (m *Manager) PublishRuntimeEvent(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.ID == "" {
		event.ID = fmt.Sprintf("pixel-runtime-%d", time.Now().UnixNano())
	}
	if event.Time == 0 {
		event.Time = time.Now().UnixMilli()
	}
	event.Revision = m.snapshot.Revision
	m.publishLocked(event)
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.snapshot = defaultSnapshot()
		m.ensureStarterKitLocked()
		return m.persistLocked()
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &m.snapshot); err != nil {
		return fmt.Errorf("decode pixel world: %w", err)
	}
	if m.snapshot.Players == nil || len(m.snapshot.Players) == 0 {
		m.snapshot = defaultSnapshot()
		m.ensureStarterKitLocked()
		return m.persistLocked()
	}
	m.ensureStarterKitLocked()
	return nil
}

func (m *Manager) ensureStarterKitLocked() {
	player, ok := m.snapshot.Players["p1"]
	if !ok {
		return
	}
	if _, found := findItem(player.Inventory, "staff-rift"); found {
		return
	}
	player.Inventory = append(player.Inventory, Item{ID: "staff-rift", Name: "破界法杖", Kind: "terrain_breaker", Quantity: 1, State: map[string]any{"sprite": "staff"}})
	m.snapshot.Players["p1"] = player
}

func (m *Manager) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

func (m *Manager) recordLocked(eventType, title, detail, playerID string) Event {
	m.snapshot.Revision++
	event := Event{
		ID: fmt.Sprintf("pixel-%d-%d", time.Now().UnixNano(), m.snapshot.Revision), Type: eventType,
		Title: title, Detail: detail, Player: playerID, Revision: m.snapshot.Revision, Time: time.Now().UnixMilli(),
	}
	m.snapshot.EventLog = append([]Event{event}, m.snapshot.EventLog...)
	if len(m.snapshot.EventLog) > 32 {
		m.snapshot.EventLog = m.snapshot.EventLog[:32]
	}
	return event
}

func (m *Manager) publishLocked(event Event) {
	for _, ch := range m.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func defaultSnapshot() Snapshot {
	return Snapshot{
		Revision: 1, SceneID: "forest-ruin", Size: Position{X: MapWidth, Y: MapHeight},
		Players: map[string]Player{"p1": {ID: "p1", Name: "爱丽丝", Position: Position{X: 14, Y: 10}}},
		Entities: []Entity{
			{ID: "chest-amber", Kind: "chest", Name: "琥珀补给箱", Position: Position{X: 8, Y: 8}, State: map[string]any{"opened": false}},
			{ID: "gate-ruin", Kind: "gate", Name: "遗迹石门", Position: Position{X: 22, Y: 5}, State: map[string]any{"opened": false}},
			{ID: "npc-scout", Kind: "npc", Name: "巡林侦察员", Position: Position{X: 17, Y: 14}, State: map[string]any{"spoken": false}},
			{ID: "crystal-aqua", Kind: "crystal", Name: "传送水晶", Position: Position{X: 5, Y: 16}, State: map[string]any{"active": true}},
		},
		ChangedTiles: map[string]string{}, EventLog: []Event{},
	}
}

func applyInteraction(entity *Entity) (string, string, string) {
	switch entity.Kind {
	case "chest":
		entity.State["opened"] = true
		return "补给箱开启", "获得了遗迹中的补给。", "entity_updated"
	case "gate":
		entity.State["opened"] = true
		return "遗迹石门开启", "阻挡已解除。", "entity_updated"
	case "npc":
		entity.State["spoken"] = true
		return "获得区域情报", "侦察员标记了一条安全路线。", "entity_updated"
	case "crystal":
		active, _ := entity.State["active"].(bool)
		entity.State["active"] = !active
		return "水晶频率改变", "传送水晶的光芒随之变化。", "entity_updated"
	default:
		return "完成交互", entity.Name + " 的状态已更新。", "entity_updated"
	}
}

func isAdjacent(a, b Position) bool { return distance(a, b) == 1 }
func distance(a, b Position) int    { return abs(a.X-b.X) + abs(a.Y-b.Y) }
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func isBlocked(position Position, snapshot Snapshot) bool {
	if position.X < 0 || position.Y < 0 || position.X >= MapWidth || position.Y >= MapHeight {
		return true
	}
	if kind := tileKind(position, snapshot); kind == "water" || kind == "tree" {
		return true
	}
	for _, entity := range snapshot.Entities {
		if entity.Position == position && entity.Kind == "gate" && entity.State["opened"] != true && entity.State["destroyed"] != true {
			return true
		}
	}
	return false
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	data, _ := json.Marshal(snapshot)
	var clone Snapshot
	_ = json.Unmarshal(data, &clone)
	sort.Slice(clone.Entities, func(i, j int) bool { return clone.Entities[i].ID < clone.Entities[j].ID })
	return clone
}

func findItem(items []Item, itemID string) (Item, bool) {
	for _, item := range items {
		if item.ID == itemID && item.Quantity > 0 {
			return item, true
		}
	}
	return Item{}, false
}

func tileKey(position Position) string { return fmt.Sprintf("%d,%d", position.X, position.Y) }
