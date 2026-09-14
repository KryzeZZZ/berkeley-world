package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type OutcomeResult struct {
	Narration string   `json:"narration"`
	Items     []string `json:"items,omitempty"`
}

type OutcomeObjectContext struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Tags        []string       `json:"tags,omitempty"`
	Layer       string         `json:"layer,omitempty"`
	State       map[string]any `json:"state,omitempty"`
	Description string         `json:"description,omitempty"`
}

type OutcomeContext struct {
	SceneID          string                 `json:"scene_id,omitempty"`
	CurrentLayer     string                 `json:"current_layer,omitempty"`
	ChildLayers      []string               `json:"child_layers,omitempty"`
	SceneDescription string                 `json:"scene_description,omitempty"`
	PlayerID         string                 `json:"player_id,omitempty"`
	PlayerLayers     []string               `json:"player_layers,omitempty"`
	NearbyObjects    []OutcomeObjectContext `json:"nearby_objects,omitempty"`
	KnownItems       []OutcomeObjectContext `json:"known_items,omitempty"`
}

type OutcomeRequest struct {
	Interaction string         `json:"interaction"`
	Instruction string         `json:"instruction"`
	Target      string         `json:"target"`
	DiceExpr    string         `json:"dice_expr"`
	DiceTotal   int            `json:"dice_total"`
	Context     OutcomeContext `json:"context,omitempty"`
}

var itemBracketPattern = regexp.MustCompile(`\[(.*?)\]`)

func CallAIOutcomeNarrator(req OutcomeRequest) (OutcomeResult, error) {
	if strings.TrimSpace(req.Interaction) == "" {
		return OutcomeResult{}, fmt.Errorf("interaction is required")
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return OutcomeResult{}, err
	}
	if cfg.Mode == "openai_compatible" && cfg.URL != "" && cfg.Model != "" {
		result, callErr := callOpenAIOutcome(cfg, req)
		if callErr != nil {
			return OutcomeResult{}, fmt.Errorf("outcome ai call failed: %w", callErr)
		}
		return result, nil
	}
	return fallbackOutcome(req), nil
}

func loadOutcomeConfig() (Config, error) {
	cfgPath := "config/ai_referee.json"
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_CONFIG")); v != "" {
		cfgPath = v
	}
	return LoadConfigFromFile(cfgPath)
}

func callOpenAIOutcome(cfg Config, reqData OutcomeRequest) (OutcomeResult, error) {
	reqJSON, _ := json.Marshal(reqData)
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Generate TRPG outcome JSON only with keys narration and items. " +
					"Items must be explicit concrete object names (e.g., 绾㈢粧甯? 閾舵垝鎸? old key). " +
					"Do NOT use vague terms like 鍑犱欢鐗╁搧/涓€浜涗笢瑗?items/loot/stuff. " +
					"Treat any interactive noun as an item candidate, not only pickable loot. " +
					"Use context.scene_description/context.nearby_objects/context.known_items and object state/description as grounding. " +
					"For scene_refresh_no_loot, focus on context.current_layer, context.child_layers, and context.nearby_objects only. " +
					"For scene_bootstrap, include key interactive scene nouns even if not picked up. " +
					"When possible, prefer existing names from context.known_items instead of inventing new ones. " +
					"For open_container in this test environment, treat action as successful and provide 1-4 concrete item names. " +
					"Narration should mention those names.",
			},
			{"role": "user", "content": string(reqJSON)},
		},
		"temperature": 0.3,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "outcome_result",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"narration": map[string]any{"type": "string"},
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"narration", "items"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return OutcomeResult{}, fmt.Errorf("marshal outcome request failed: %w", err)
	}
	client := &http.Client{Timeout: cfg.Timeout}
	httpReq, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return OutcomeResult{}, fmt.Errorf("create outcome request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return OutcomeResult{}, fmt.Errorf("call outcome model failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OutcomeResult{}, fmt.Errorf("outcome model status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return OutcomeResult{}, fmt.Errorf("decode outcome wrapper failed: %w", err)
	}
	if len(wrapper.Choices) == 0 {
		return OutcomeResult{}, fmt.Errorf("outcome response has no choices")
	}
	content := extractJSONObject(strings.TrimSpace(wrapper.Choices[0].Message.Content))
	out, err := decodeOutcomeResultLoose([]byte(content))
	if err != nil {
		return OutcomeResult{}, fmt.Errorf("decode outcome content failed: %w", err)
	}
	if strings.TrimSpace(out.Narration) == "" {
		return OutcomeResult{}, fmt.Errorf("outcome narration is empty")
	}
	out.Items = sanitizeItems(out.Items)
	return out, nil
}

func RefineObtainedItemsWithAI(req OutcomeRequest, narration string, candidates []string) ([]string, error) {
	narration = strings.TrimSpace(narration)
	if narration == "" {
		return nil, nil
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return nil, nil
	}
	prompt := map[string]any{
		"instruction": req.Instruction,
		"target":      req.Target,
		"dice_expr":   req.DiceExpr,
		"dice_total":  req.DiceTotal,
		"narration":   narration,
		"candidates":  candidates,
	}
	content, err := callOutcomeJSON(cfg, "extract_obtained_items",
		"Extract only items that are actually obtained now. "+
			"Treat interactive concrete nouns as item candidates, not only pickable loot labels. "+
			"Use context.nearby_objects/context.known_items state and description to disambiguate names. "+
			"If narration says need/fail/not obtained (e.g. 闇€瑕侀挜鍖?, do NOT include that object. "+
			"Return JSON with items string array only.", prompt)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return sanitizeItems(result.Items), nil
}

func GenerateLootItemsWithAI(req OutcomeRequest, narration string) ([]string, error) {
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return nil, nil
	}
	prompt := map[string]any{
		"instruction": req.Instruction,
		"target":      req.Target,
		"dice_expr":   req.DiceExpr,
		"dice_total":  req.DiceTotal,
		"narration":   narration,
		"context":     req.Context,
	}
	content, err := callOutcomeJSON(cfg, "generate_test_loot",
		"For testing, generate 1-3 concrete item names in JSON items array. "+
			"Any interactive noun can be an item candidate; do not limit to pickable loot wording. "+
			"Use context.scene_description/context.nearby_objects/context.known_items and their state/description to keep items coherent with the scene. "+
			"Prefer names from context.known_items when suitable. "+
			"No vague words.", prompt)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return sanitizeItems(result.Items), nil
}

func ExtractItemsFromNarrationWithAI(narration string) ([]string, error) {
	narration = strings.TrimSpace(narration)
	if narration == "" {
		return nil, nil
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return nil, nil
	}
	content, err := callOutcomeJSON(cfg, "item_extract",
		"Extract explicit concrete interactive object names from narration. "+
			"Treat any interactive noun as an item candidate, not only pickable objects. "+
			"Include concrete props/materials such as 绾㈢粧甯?when interactable in context. "+
			"Do not output vague terms.", narration)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return sanitizeItems(result.Items), nil
}

func ExtractInteractiveItemsFromNarrationWithAI(req OutcomeRequest, narration string) ([]string, error) {
	narration = strings.TrimSpace(narration)
	if narration == "" {
		return nil, nil
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return nil, nil
	}
	prompt := map[string]any{
		"interaction": req.Interaction,
		"instruction": req.Instruction,
		"narration":   narration,
		"context":     req.Context,
	}
	content, err := callOutcomeJSON(cfg, "interactive_item_extract",
		"Extract concrete interactive nouns as item names from narration/context. "+
			"Not limited to pickable or obtained loot. "+
			"Prioritize interactable scene props and named objects. "+
			"Use context.nearby_objects/context.known_items state and description as evidence. "+
			"Do not output vague terms.", prompt)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return sanitizeItems(result.Items), nil
}

func callOutcomeJSON(cfg Config, schemaName, systemPrompt string, user any) (string, error) {
	userBytes, _ := json.Marshal(user)
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": string(userBytes)},
		},
		"temperature": 0.0,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   schemaName,
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"items"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: cfg.Timeout}
	httpReq, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("model status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return "", err
	}
	if len(wrapper.Choices) == 0 {
		return "", nil
	}
	return extractJSONObject(strings.TrimSpace(wrapper.Choices[0].Message.Content)), nil
}

func fallbackOutcome(req OutcomeRequest) OutcomeResult {
	if req.Interaction == "open_container" {
		return OutcomeResult{
			Narration: fmt.Sprintf("You try to open the container (%s=%d), but the outcome service is unavailable.", req.DiceExpr, req.DiceTotal),
			Items:     []string{},
		}
	}
	return OutcomeResult{Narration: "You completed the action."}
}

func ExtractItemNamesFromNarration(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	matches := itemBracketPattern.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		name := strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		name = strings.Trim(name, "锛?銆?!? ")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) > 0 {
		return sanitizeItems(out)
	}
	heuristic := []string{"red cloth", "cloth", "key", "ring", "coin", "gem", "scroll", "potion", "dagger", "short sword"}
	for _, token := range heuristic {
		if strings.Contains(strings.ToLower(text), strings.ToLower(token)) {
			out = append(out, token)
		}
	}
	return sanitizeItems(out)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func uniqueNonEmpty(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func sanitizeItems(items []string) []string {
	bad := []string{
		"items", "stuff", "loot", "something", "objects", "item",
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, "[]\"'.,!? ")
		if item == "" {
			continue
		}
		lower := strings.ToLower(item)
		skip := false
		for _, b := range bad {
			if lower == strings.ToLower(b) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out = append(out, item)
	}
	return uniqueNonEmpty(out)
}

func decodeOutcomeResultLoose(raw []byte) (OutcomeResult, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return OutcomeResult{}, err
	}
	out := OutcomeResult{
		Narration: strings.TrimSpace(asStringAny(obj["narration"])),
		Items:     normalizeItems(obj["items"]),
	}
	return out, nil
}

func normalizeItems(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch x := item.(type) {
		case string:
			x = strings.TrimSpace(x)
			if x != "" {
				out = append(out, x)
			}
		case map[string]any:
			name := strings.TrimSpace(asStringAny(x["name"]))
			if name != "" {
				out = append(out, name)
			}
		}
	}
	return uniqueNonEmpty(out)
}

func asStringAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func NormalizeItemsToChineseWithAI(req OutcomeRequest, items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return nil, nil
	}
	prompt := map[string]any{
		"items":   items,
		"context": req.Context,
	}
	content, err := callOutcomeJSON(cfg, "normalize_items_zh",
		"Normalize item names to simplified Chinese nouns only. "+
			"Prefer reusing existing Chinese names from context.known_items. "+
			"Output JSON with items only.", prompt)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return sanitizeItems(result.Items), nil
}
