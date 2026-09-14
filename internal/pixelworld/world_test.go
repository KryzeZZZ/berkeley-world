package pixelworld

import (
	"path/filepath"
	"testing"
)

func TestMovePersistsToLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pixel_world.json")
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}

	_, event, err := manager.Move("p1", Position{X: 15, Y: 10})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "entity_moved" {
		t.Fatalf("event type = %q", event.Type)
	}

	reloaded, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	player := reloaded.Snapshot().Players["p1"]
	if player.Position != (Position{X: 15, Y: 10}) {
		t.Fatalf("position = %#v", player.Position)
	}
}

func TestInteractRequiresAdjacentEntity(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "pixel_world.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Interact("p1", "chest-amber"); err == nil {
		t.Fatal("expected distance error")
	}

	if _, _, err := manager.Move("p1", Position{X: 13, Y: 10}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Move("p1", Position{X: 13, Y: 9}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Move("p1", Position{X: 13, Y: 8}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Move("p1", Position{X: 12, Y: 8}); err == nil {
		t.Fatal("expected water collision")
	}
}

func TestInteractChangesEntityState(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "pixel_world.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.snapshot.Players["p1"] = Player{ID: "p1", Name: "爱丽丝", Position: Position{X: 8, Y: 9}}
	manager.mu.Unlock()

	_, event, err := manager.Interact("p1", "chest-amber")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "entity_updated" {
		t.Fatalf("event type = %q", event.Type)
	}
	entity := manager.Snapshot().Entities[0]
	if opened, _ := entity.State["opened"].(bool); !opened {
		t.Fatalf("chest state = %#v", entity.State)
	}
}

func TestStaffBreaksAdjacentTreeAndPersistsTile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pixel_world.json")
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	player := manager.snapshot.Players["p1"]
	player.Position = Position{X: 2, Y: 2}
	manager.snapshot.Players["p1"] = player
	manager.mu.Unlock()

	world, event, err := manager.UseItem("p1", "staff-rift", Position{X: 1, Y: 2})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "tile_changed" {
		t.Fatalf("event type = %q", event.Type)
	}
	if world.ChangedTiles["1,2"] != "rubble-tree" {
		t.Fatalf("changed tiles = %#v", world.ChangedTiles)
	}
	if tileKind(Position{X: 1, Y: 2}, world) != "rubble-tree" {
		t.Fatal("tile did not become rubble")
	}
}
