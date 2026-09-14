package ai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type fileConfig struct {
	Mode         string `json:"mode"`
	URL          string `json:"url"`
	Token        string `json:"token"`
	Model        string `json:"model"`
	Timeout      string `json:"timeout"`
	SystemPrompt string `json:"system_prompt"`
}

func NewOptionalRefereeFromConfigFile(path string) (Referee, string, error) {
	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		return nil, "", err
	}
	return NewOptionalReferee(cfg)
}

func LoadConfigFromFile(path string) (Config, error) {
	cfg := Config{
		Mode:         "rule",
		Timeout:      40 * time.Second,
		SystemPrompt: defaultSystemPrompt,
	}
	if strings.TrimSpace(path) == "" {
		return applyEnvOverrides(cfg)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return applyEnvOverrides(cfg)
		}
		return Config{}, fmt.Errorf("read config file failed: %w", err)
	}
	var fc fileConfig
	if err := json.Unmarshal(content, &fc); err != nil {
		return Config{}, fmt.Errorf("parse config file failed: %w", err)
	}

	if v := strings.TrimSpace(fc.Mode); v != "" {
		cfg.Mode = v
	}
	if v := strings.TrimSpace(fc.URL); v != "" {
		cfg.URL = v
	}
	if v := strings.TrimSpace(fc.Token); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(fc.Model); v != "" {
		cfg.Model = v
	}
	if v := strings.TrimSpace(fc.SystemPrompt); v != "" {
		cfg.SystemPrompt = v
	}
	if v := strings.TrimSpace(fc.Timeout); v != "" {
		timeout, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid timeout in config file: %w", err)
		}
		cfg.Timeout = timeout
	}
	return applyEnvOverrides(cfg)
}

func applyEnvOverrides(cfg Config) (Config, error) {
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_MODE")); v != "" {
		cfg.Mode = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_URL")); v != "" {
		cfg.URL = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_TOKEN")); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_MODEL")); v != "" {
		cfg.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_SYSTEM_PROMPT")); v != "" {
		cfg.SystemPrompt = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_TIMEOUT")); v != "" {
		timeout, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid AI_REFEREE_TIMEOUT: %w", err)
		}
		cfg.Timeout = timeout
	}
	cfg.URL = normalizeOpenAICompatibleChatURL(cfg.Mode, cfg.URL)
	return cfg, nil
}

func normalizeOpenAICompatibleChatURL(mode, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.EqualFold(strings.TrimSpace(mode), "openai_compatible") || rawURL == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return rawURL
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		parsed.Path = path
		return parsed.String()
	}
	parsed.Path = path + "/chat/completions"
	return parsed.String()
}
