package main

import (
	"log"
	"net/http"

	"SAO/internal/api"
	"SAO/internal/pixelapi"
)

// pixel_server serves the Unity/Phaser pixel client without loading the AI or
// database configuration used by the broader TRPG service.
func main() {
	handler, err := pixelapi.NewPixelHandler("data/pixel_world.json")
	if err != nil {
		log.Fatalf("init pixel server failed: %v", err)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	const address = "127.0.0.1:8080"
	log.Printf("pixel-only server listening on http://%s", address)
	if err := http.ListenAndServe(address, api.WithLocalDevelopmentCORS(mux)); err != nil {
		log.Fatalf("pixel-only server stopped: %v", err)
	}
}
