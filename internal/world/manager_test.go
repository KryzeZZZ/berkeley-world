package world

import (
	"path/filepath"
	"testing"
	"time"

	"SAO/internal/model"
	"SAO/internal/scene"
)

func TestAddPlayerWithPersistenceDoesNotDeadlock(t *testing.T) {
	manager := NewManager(nil)
	manager.EnablePersistence(filepath.Join(t.TempDir(), "world.json"))
	manager.AddScene(scene.NewSceneInstance("scene-test", 8, nil))

	done := make(chan error, 1)
	go func() {
		done <- manager.AddPlayer("scene-test", &model.PlayerState{ID: "p1", Name: "Tester"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddPlayer() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AddPlayer() deadlocked while persistence was enabled")
	}
}

func TestCurrentSurroundingsQueriesBypassSemanticLocationMatching(t *testing.T) {
	for _, query := range []string{"四周", "周围", "附近", "这里", "当前区域", "surroundings", "here"} {
		if !isCurrentSurroundingsQuery(query) {
			t.Errorf("isCurrentSurroundingsQuery(%q) = false", query)
		}
	}
	for _, query := range []string{"酒馆", "地牢", "北门"} {
		if isCurrentSurroundingsQuery(query) {
			t.Errorf("isCurrentSurroundingsQuery(%q) = true", query)
		}
	}
}
