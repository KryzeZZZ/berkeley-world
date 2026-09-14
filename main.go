package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"SAO/internal/ai"
	"SAO/internal/api"
	"SAO/internal/config"
	"SAO/internal/model"
	"SAO/internal/scene"
	"SAO/internal/world"
)

func main() {
	_ = config.LoadEnvFile(envFilePath())

	worldManager := world.NewManager(nil)
	worldManager.EnablePersistence("data/world.json")
	scenePolicy, err := config.LoadScenePolicy("config/scene_policy.json")
	if err != nil {
		log.Fatalf("load scene policy failed: %v", err)
	}
	worldManager.SetAllowSceneCreation(scenePolicy.AllowSceneCreation)

	loaded, err := worldManager.LoadFromPersistence()
	if err != nil {
		log.Fatalf("load persistence failed: %v", err)
	}
	if !loaded {
		sceneInstance := scene.NewSceneInstance("scene-town", 128, nil)
		sceneInstance.RegisterLayer("scene-dungeon")
		worldManager.AddScene(sceneInstance)

		if err := worldManager.AddPlayer("scene-town", &model.PlayerState{ID: "p1", Name: "Alice"}); err != nil {
			log.Fatalf("add player failed: %v", err)
		}
		if err := worldManager.AddObject("scene-town", &model.GameObject{
			ID: "obj-goblin", Name: "Goblin", Tags: []string{"npc", "hostile", "goblin", "\u54e5\u5e03\u6797"},
			State: map[string]any{"hp": 10, "layer": "scene-town"}, Version: 1,
		}); err != nil {
			log.Fatalf("add object failed: %v", err)
		}
		if err := worldManager.AddObject("scene-town", &model.GameObject{
			ID: "obj-chest-1", Name: "Wooden Chest", Tags: []string{"container", "chest", "\u7bb1\u5b50", "\u5b9d\u7bb1"},
			State: map[string]any{"locked": false, "opened": false, "layer": "scene-town"}, Version: 1,
		}); err != nil {
			log.Fatalf("add chest failed: %v", err)
		}
	}

	configPath := os.Getenv("AI_REFEREE_CONFIG")
	if strings.TrimSpace(configPath) == "" {
		configPath = "config/ai_referee.json"
	}
	referee, mode, err := ai.NewOptionalRefereeFromConfigFile(configPath)
	if err != nil {
		log.Fatalf("init ai referee failed: %v", err)
	}
	log.Printf("ai referee mode: %s (config=%s)", mode, configPath)

	mux := http.NewServeMux()
	api.NewHandler(worldManager, referee).RegisterRoutes(mux)

	addr := ":8080"
	log.Printf("server listening on %s", addr)
	if err := http.ListenAndServe(addr, api.WithLocalDevelopmentCORS(mux)); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}
