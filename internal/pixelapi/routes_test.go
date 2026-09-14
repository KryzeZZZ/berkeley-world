package pixelapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestPixelRoutesExcludeTextAPI(t *testing.T) {
	handler, err := NewPixelHandler(filepath.Join(t.TempDir(), "pixel_world.json"))
	if err != nil {
		t.Fatalf("NewPixelHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("text route status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
