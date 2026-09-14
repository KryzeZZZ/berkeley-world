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
		sceneInstance := scene.NewSceneInstance("scene-城镇", 128, nil)
		sceneInstance.RegisterLayer("scene-地牢")
		worldManager.AddScene(sceneInstance)

		if err := worldManager.AddPlayer("scene-城镇", &model.PlayerState{
			ID:   "p1",
			Name: "爱丽丝",
		}); err != nil {
			log.Fatalf("add player failed: %v", err)
		}

		if err := worldManager.AddObject("scene-城镇", &model.GameObject{
			ID:   "obj-哥布林",
			Name: "哥布林",
			Tags: []string{"非玩家角色", "敌对", "哥布林"},
			State: map[string]any{
				"hp":    10,
				"layer": "scene-城镇",
			},
			Version: 1,
		}); err != nil {
			log.Fatalf("add object failed: %v", err)
		}
		if err := worldManager.AddObject("scene-城镇", &model.GameObject{
			ID:   "obj-木箱-1",
			Name: "木箱",
			Tags: []string{"容器", "木箱", "箱子"},
			State: map[string]any{
				"locked": false,
				"opened": false,
				"layer":  "scene-城镇",
			},
			Version: 1,
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
	handler := api.NewHandler(worldManager, referee)
	handler.RegisterRoutes(mux)

	addr := strings.TrimSpace(os.Getenv("SERVER_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
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
