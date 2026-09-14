package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SAO/internal/ai"
	"SAO/internal/scene"
	"SAO/internal/world"
)

func TestTextRoutesExcludePixelAPI(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(world.NewManager(nil), ai.NewRuleReferee()).RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, "/pixel/world", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("pixel route status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestLoginDefaultsToStarterTown(t *testing.T) {
	manager := world.NewManager(nil)
	manager.AddScene(scene.NewSceneInstance(defaultSceneID, 8, nil))
	mux := http.NewServeMux()
	NewHandler(manager, ai.NewRuleReferee()).RegisterRoutes(mux)

	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"player_id":"new-player","device_id":"test-device"}`),
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	playerScene, err := manager.GetSceneByPlayer("new-player")
	if err != nil {
		t.Fatalf("GetSceneByPlayer() error = %v", err)
	}
	if playerScene.ID != defaultSceneID {
		t.Fatalf("scene ID = %q, want %q", playerScene.ID, defaultSceneID)
	}
}
