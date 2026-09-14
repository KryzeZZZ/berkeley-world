package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"strings"
)

type LayerPlanInput struct {
	RawInput     string   `json:"raw_input"`
	Requested    string   `json:"requested_layer"`
	ActionType   string   `json:"action_type"`
	CurrentLayer string   `json:"current_layer"`
	PlayerLayers []string `json:"player_layers,omitempty"`
	KnownLayers  []string `json:"known_layers,omitempty"`
}

type LayerPlan struct {
	LayerID  string `json:"layer_id"`
	Relation string `json:"relation"` // child|sibling|parent|same
	Reason   string `json:"reason,omitempty"`
}

func CallAILayerPlanner(input LayerPlanInput) (LayerPlan, error) {
	cfgPath := strings.TrimSpace(os.Getenv("AI_REFEREE_CONFIG"))
	if cfgPath == "" {
		cfgPath = "config/ai_referee.json"
	}
	cfg, err := LoadConfigFromFile(cfgPath)
	if err != nil {
		return LayerPlan{}, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return fallbackLayerPlan(input), nil
	}
	reqJSON, _ := json.Marshal(input)
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Plan TRPG layer transition. Return JSON only with layer_id,relation,reason. " +
					"relation enum: child,sibling,parent,same. Prefer existing known layers when possible.",
			},
			{"role": "user", "content": string(reqJSON)},
		},
		"temperature": 0.1,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "layer_plan",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"layer_id": map[string]any{"type": "string"},
						"relation": map[string]any{"type": "string", "enum": []string{"child", "sibling", "parent", "same"}},
						"reason":   map[string]any{"type": "string"},
					},
					"required": []string{"layer_id", "relation"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return LayerPlan{}, err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return LayerPlan{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fallbackLayerPlan(input), nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fallbackLayerPlan(input), nil
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper.Choices) == 0 {
		return fallbackLayerPlan(input), nil
	}
	content := extractJSONObject(strings.TrimSpace(wrapper.Choices[0].Message.Content))
	var plan LayerPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return fallbackLayerPlan(input), nil
	}
	plan.LayerID = sanitizeLayerID(plan.LayerID, input.Requested)
	if plan.Relation == "" {
		plan.Relation = "child"
	}
	return plan, nil
}

func fallbackLayerPlan(input LayerPlanInput) LayerPlan {
	return LayerPlan{
		LayerID:  sanitizeLayerID(input.Requested, input.RawInput),
		Relation: "child",
		Reason:   "fallback",
	}
}

func sanitizeLayerID(primary, backup string) string {
	raw := strings.TrimSpace(primary)
	if raw == "" {
		raw = strings.TrimSpace(backup)
	}
	raw = strings.ToLower(raw)
	raw = strings.ReplaceAll(raw, " ", "-")
	buf := make([]rune, 0, len(raw))
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			buf = append(buf, r)
		}
	}
	id := strings.Trim(string(buf), "-_")
	if id == "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(raw))
		id = fmt.Sprintf("custom-%x", h.Sum32())
	}
	if !strings.HasPrefix(id, "scene-") {
		id = "scene-" + id
	}
	return id
}
