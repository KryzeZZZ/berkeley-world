package scene

import (
	"testing"

	"SAO/internal/ai"
	"SAO/internal/model"
)

func TestBuildObservePayloadIncludesVisibleObjects(t *testing.T) {
	req := ai.OutcomeRequest{Context: ai.OutcomeContext{
		CurrentLayer: "town",
		ChildLayers:  []string{"inn"},
		NearbyObjects: []ai.OutcomeObjectContext{
			{ID: "goblin", Name: "Goblin", Layer: "town"},
			{ID: "chest", Name: "Chest", Layer: "town"},
		},
	}}

	payload := buildObservePayload(req, "You look around.")
	visible, ok := payload["visible_objects"].([]ai.OutcomeObjectContext)
	if !ok {
		t.Fatalf("visible_objects type = %T", payload["visible_objects"])
	}
	if len(visible) != 2 {
		t.Fatalf("visible_objects count = %d, want 2", len(visible))
	}
	if payload["current_layer"] != "town" {
		t.Fatalf("current_layer = %v, want town", payload["current_layer"])
	}
}

func TestNearbyIncludesObjectsFromParentCurrentAndChildLayers(t *testing.T) {
	scene := NewSceneInstance("root", 8, nil)
	scene.AddPlayer(&model.PlayerState{ID: "p1", LayerStack: []string{"root", "town"}})
	scene.RegisterLayer("cellar")
	scene.setLayerParent("town", "root")
	scene.setLayerParent("cellar", "town")
	scene.AddObject(&model.GameObject{ID: "root-object", Name: "Root object", State: map[string]any{"layer": "root"}})
	scene.AddObject(&model.GameObject{ID: "town-object", Name: "Town object", State: map[string]any{"layer": "town"}})
	scene.AddObject(&model.GameObject{ID: "cellar-object", Name: "Cellar object", State: map[string]any{"layer": "cellar"}})

	nearby, err := scene.GetNearbyLayersAndObjects("p1")
	if err != nil {
		t.Fatalf("GetNearbyLayersAndObjects() error = %v", err)
	}
	layerObjects, ok := nearby["layer_objects"].(map[string][]map[string]any)
	if !ok {
		t.Fatalf("layer_objects type = %T", nearby["layer_objects"])
	}
	for _, layerID := range []string{"root", "town", "cellar"} {
		if got := len(layerObjects[layerID]); got != 1 {
			t.Errorf("objects in %s = %d, want 1", layerID, got)
		}
	}
}
