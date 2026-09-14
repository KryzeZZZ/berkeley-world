package semantic

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	URL      string
	Model    string
	Token    string
	Timeout  time.Duration
	MinScore float64
	MinGap   float64
}

type fileConfig struct {
	URL      string `json:"url"`
	Model    string `json:"model"`
	Token    string `json:"token"`
	Timeout  string `json:"timeout"`
	MinScore string `json:"min_score"`
	MinGap   string `json:"min_gap"`
}

func DefaultConfig() Config {
	return Config{
		Model:    "text-embedding-3-small",
		Timeout:  40 * time.Second,
		MinScore: 0.40,
		MinGap:   0.04,
	}
}

func LoadConfigFromFile(path string) (Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read semantic config failed: %w", err)
	}
	content = stripUTF8BOM(content)
	var fc fileConfig
	if err := json.Unmarshal(content, &fc); err != nil {
		return Config{}, fmt.Errorf("parse semantic config failed: %w", err)
	}
	if v := strings.TrimSpace(fc.URL); v != "" {
		cfg.URL = v
	}
	if v := strings.TrimSpace(fc.Model); v != "" {
		cfg.Model = v
	}
	if v := strings.TrimSpace(fc.Token); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(fc.Timeout); v != "" {
		timeout, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid timeout in semantic config: %w", err)
		}
		cfg.Timeout = timeout
	}
	if v := strings.TrimSpace(fc.MinScore); v != "" {
		value, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid min_score in semantic config: %w", err)
		}
		cfg.MinScore = value
	}
	if v := strings.TrimSpace(fc.MinGap); v != "" {
		value, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid min_gap in semantic config: %w", err)
		}
		cfg.MinGap = value
	}
	return cfg, nil
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}
