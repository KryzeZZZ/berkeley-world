package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"SAO/internal/ai"
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
