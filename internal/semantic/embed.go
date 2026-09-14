package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// EmbedTexts calls the configured embedding endpoint and returns vectors in input order.
func EmbedTexts(ctx context.Context, inputs []string) ([][]float64, error) {
	configPath := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_CONFIG"))
	if configPath == "" {
		configPath = "config/layer_semantic.json"
	}
	cfg, err := LoadConfigFromFile(configPath)
	if err != nil {
		return nil, err
	}

	rawURL := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_URL"))
	if rawURL == "" {
		rawURL = strings.TrimSpace(cfg.URL)
	}
	if rawURL == "" {
		return nil, fmt.Errorf("embedding url is required")
	}
	model := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_MODEL"))
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	token := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_TOKEN"))
	if token == "" {
		token = cfg.Token
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 40 * time.Second
	}

	payload := map[string]any{
		"model": model,
		"input": inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding API status %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response failed: %w", err)
	}
	out := make([][]float64, len(inputs))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(inputs) {
			return nil, fmt.Errorf("embedding index out of range: %d", item.Index)
		}
		out[item.Index] = item.Embedding
	}
	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("missing embedding for input index %d", i)
		}
	}
	return out, nil
}
