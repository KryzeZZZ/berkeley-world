package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"SAO/internal/config"
	"SAO/internal/world"
)

func main() {
	_ = config.LoadEnvFile(envFilePath())

	dsn := strings.TrimSpace(os.Getenv("WORLD_PG_DSN"))
	if dsn == "" {
		fmt.Println("WORLD_PG_DSN is required")
		os.Exit(1)
	}
	worldID := strings.TrimSpace(os.Getenv("WORLD_ID"))
	if worldID == "" {
		worldID = "default"
	}
	path := strings.TrimSpace(os.Getenv("WORLD_JSON_PATH"))
	if path == "" {
		path = "data/world.json"
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("read world json failed: %v\n", err)
		os.Exit(1)
	}
	raw = stripUTF8BOM(raw)
	var worldData map[string]any
	if err := json.Unmarshal(raw, &worldData); err != nil {
		fmt.Printf("parse world json failed: %v\n", err)
		os.Exit(1)
	}

	if err := world.SaveWorldSnapshotToDB(dsn, worldID, worldData); err != nil {
		fmt.Printf("import world json failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("world json imported to db (world_id=%s)\n", worldID)
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}
