package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type ScenePolicy struct {
	AllowSceneCreation bool `json:"allow_scene_creation"`
}

func LoadScenePolicy(path string) (ScenePolicy, error) {
	policy := ScenePolicy{AllowSceneCreation: true}
	if strings.TrimSpace(path) == "" {
		path = "config/scene_policy.json"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return policy, nil
		}
		return ScenePolicy{}, fmt.Errorf("read scene policy failed: %w", err)
	}
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	if err := json.Unmarshal(content, &policy); err != nil {
		return ScenePolicy{}, fmt.Errorf("parse scene policy failed: %w", err)
	}
	return policy, nil
}
