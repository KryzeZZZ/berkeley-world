package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type RollJudgeRequest struct {
	Instruction       string   `json:"instruction"`
	ActionType        string   `json:"action_type"`
	Interaction       string   `json:"interaction"`
	Target            string   `json:"target"`
	Attribute         string   `json:"attribute"`
	BaseRoll          int      `json:"base_roll"`
	AttributeModifier int      `json:"attribute_modifier"`
	InventoryModifier int      `json:"inventory_modifier"`
	CurrentLayer      string   `json:"current_layer"`
	InventoryItems    []string `json:"inventory_items,omitempty"`
}

type RollJudgeResult struct {
	Modifier int    `json:"modifier"`
	Reason   string `json:"reason"`
}

func JudgeRollModifier(req RollJudgeRequest) (RollJudgeResult, error) {
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return heuristicRollJudge(req), nil
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return heuristicRollJudge(req), nil
	}

	user, _ := json.Marshal(req)
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Judge TRPG action reasonableness and return JSON only with modifier and reason. " +
					"modifier is integer in [-5,5]. " +
					"Positive means the attempt is more reasonable/prepared. " +
					"Negative means risky/unreasonable/improbable.",
			},
			{"role": "user", "content": string(user)},
		},
		"temperature": 0.1,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "roll_reasonableness",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"modifier": map[string]any{"type": "integer", "minimum": -5, "maximum": 5},
						"reason":   map[string]any{"type": "string"},
					},
					"required": []string{"modifier", "reason"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return heuristicRollJudge(req), nil
	}
	httpReq, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return heuristicRollJudge(req), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return heuristicRollJudge(req), nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return heuristicRollJudge(req), nil
	}

	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper.Choices) == 0 {
		return heuristicRollJudge(req), nil
	}
	content := extractJSONObject(strings.TrimSpace(wrapper.Choices[0].Message.Content))
	var out RollJudgeResult
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return heuristicRollJudge(req), nil
	}
	if out.Modifier > 5 {
		out.Modifier = 5
	}
	if out.Modifier < -5 {
		out.Modifier = -5
	}
	if strings.TrimSpace(out.Reason) == "" {
		out.Reason = "AI judged action reasonableness."
	}
	return out, nil
}

func heuristicRollJudge(req RollJudgeRequest) RollJudgeResult {
	text := strings.ToLower(strings.TrimSpace(req.Instruction + " " + req.Target))
	mod := 0
	reason := "Heuristic judge."
	if containsAny(text, "impossible", "不可能", "徒手", "空手", "硬撬", "reckless") {
		mod -= 2
		reason = "Action looks risky or implausible."
	}
	if containsAny(text, "carefully", "careful", "准备", "计划", "工具", "lever", "谨慎") {
		mod += 1
		reason = "Action appears prepared."
	}
	if mod > 5 {
		mod = 5
	}
	if mod < -5 {
		mod = -5
	}
	return RollJudgeResult{Modifier: mod, Reason: reason}
}

func containsAny(input string, keys ...string) bool {
	for _, key := range keys {
		if strings.Contains(input, strings.ToLower(strings.TrimSpace(key))) {
			return true
		}
	}
	return false
}
