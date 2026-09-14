package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	kwAttackCN          = "\u653b\u51fb"
	kwCheckCN1          = "\u8c03\u67e5"
	kwCheckCN2          = "\u68c0\u5b9a"
	kwCreateCN1         = "\u53ec\u5524"
	kwCreateCN2         = "\u751f\u6210"
	kwCreateCN3         = "\u521b\u5efa"
	kwAdvantageCN       = "\u4f18\u52bf"
	kwDisadvantageCN    = "\u52a3\u52bf"
	defaultSystemPrompt = "You are a TRPG action parser for scene flow and item interaction. Return one JSON object only. " +
		"Allowed type: push_layer,pop_layer,replace_layer,interact,observe,narrate. " +
		"Allowed interact interaction: open_container,pickup_item,drop_item (interaction may be empty for generic interact). " +
		"Use observe for looking/examining/inspecting objects or scenes. " +
		"For entering/leaving/switching places, use push_layer/pop_layer/replace_layer. " +
		"For opening non-container objects, use type=interact and leave interaction empty. " +
		"For opening containers, use interaction=open_container. For pick/drop, use pickup_item/drop_item. " +
		"Use narrate only when no actionable game operation exists."
)

type Referee interface {
	Call(input string) (ParsedAction, error)
}

type ParsedObject struct {
	Name  string         `json:"name"`
	Tags  []string       `json:"tags,omitempty"`
	State map[string]any `json:"state,omitempty"`
}

type ParsedAction struct {
	Type            string         `json:"type"`
	Interaction     string         `json:"interaction,omitempty"`
	TargetQuery     string         `json:"target_query,omitempty"`
	LayerID         string         `json:"layer_id,omitempty"`
	DiceExpr        string         `json:"dice_expr,omitempty"`
	UseAdvantage    bool           `json:"use_advantage,omitempty"`
	UseDisadvantage bool           `json:"use_disadvantage,omitempty"`
	Patch           map[string]any `json:"patch,omitempty"`
	CreateObject    *ParsedObject  `json:"create_object,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
}

type RuleReferee struct{}

type HTTPReferee struct {
	url    string
	token  string
	client *http.Client
}

type OpenAICompatibleReferee struct {
	url          string
	token        string
	model        string
	systemPrompt string
	client       *http.Client
}

type Config struct {
	Mode         string
	URL          string
	Token        string
	Model        string
	Timeout      time.Duration
	SystemPrompt string
}

func NewRuleReferee() *RuleReferee {
	return &RuleReferee{}
}

func NewHTTPReferee(rawURL, token string, timeout time.Duration) (*HTTPReferee, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("ai referee url is required")
	}
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return nil, fmt.Errorf("invalid ai referee url: %w", err)
	}
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	return &HTTPReferee{
		url:   rawURL,
		token: token,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func NewOptionalRefereeFromEnv() (Referee, string, error) {
	cfg := Config{
		Mode:         strings.TrimSpace(os.Getenv("AI_REFEREE_MODE")),
		URL:          strings.TrimSpace(os.Getenv("AI_REFEREE_URL")),
		Token:        strings.TrimSpace(os.Getenv("AI_REFEREE_TOKEN")),
		Model:        strings.TrimSpace(os.Getenv("AI_REFEREE_MODEL")),
		Timeout:      40 * time.Second,
		SystemPrompt: strings.TrimSpace(os.Getenv("AI_REFEREE_SYSTEM_PROMPT")),
	}
	if v := strings.TrimSpace(os.Getenv("AI_REFEREE_TIMEOUT")); v != "" {
		timeout, err := time.ParseDuration(v)
		if err != nil {
			return nil, "", fmt.Errorf("invalid AI_REFEREE_TIMEOUT: %w", err)
		}
		cfg.Timeout = timeout
	}
	return NewOptionalReferee(cfg)
}

func NewOptionalReferee(cfg Config) (Referee, string, error) {
	if cfg.Mode == "" {
		cfg.Mode = "rule"
		if cfg.URL != "" {
			cfg.Mode = "http_json"
		}
	}
	cfg.URL = normalizeOpenAICompatibleChatURL(cfg.Mode, cfg.URL)
	if cfg.Timeout <= 0 {
		cfg.Timeout = 40 * time.Second
	}
	switch cfg.Mode {
	case "rule":
		return NewRuleReferee(), "rule", nil
	case "http_json":
		if cfg.URL == "" {
			return nil, "", fmt.Errorf("AI_REFEREE_URL is required when AI_REFEREE_MODE=http_json")
		}
		httpReferee, err := NewHTTPReferee(cfg.URL, cfg.Token, cfg.Timeout)
		if err != nil {
			return nil, "", err
		}
		return httpReferee, "http_json", nil
	case "openai_compatible":
		if cfg.URL == "" {
			return nil, "", fmt.Errorf("AI_REFEREE_URL is required when AI_REFEREE_MODE=openai_compatible")
		}
		if cfg.Model == "" {
			return nil, "", fmt.Errorf("AI_REFEREE_MODEL is required when AI_REFEREE_MODE=openai_compatible")
		}
		ref, err := NewOpenAICompatibleReferee(cfg.URL, cfg.Token, cfg.Model, cfg.Timeout, cfg.SystemPrompt)
		if err != nil {
			return nil, "", err
		}
		return ref, "openai_compatible", nil
	default:
		return nil, "", fmt.Errorf("unsupported AI_REFEREE_MODE: %s", cfg.Mode)
	}
}

func CallAIReferee(input string) (ParsedAction, error) {
	ref, _, err := NewOptionalRefereeFromEnv()
	if err != nil {
		return ParsedAction{}, err
	}
	return ref.Call(input)
}

func (r *RuleReferee) Call(input string) (ParsedAction, error) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if trimmed == "" {
		return ParsedAction{}, fmt.Errorf("input is required")
	}
	lower := strings.ToLower(trimmed)
	parsed := ParsedAction{Type: "narrate", Payload: map[string]any{"raw_input": trimmed}}

	if hasAny(lower, "观察", "查看", "看看", "inspect", "examine", "look") && parsed.Type == "narrate" {
		parsed.Type = "observe"
		parsed.TargetQuery = extractEntity(trimmed, lower, []string{"观察", "查看", "看看", "inspect", "examine", "look"})
		if isSurroundingsQuery(parsed.TargetQuery) {
			parsed.TargetQuery = ""
		}
	}
	if hasAny(lower, "\u6253\u5f00", "\u5f00\u542f", "open", "unlock") && hasAny(lower, "\u7bb1\u5b50", "\u5b9d\u7bb1", "chest", "box", "crate") {
		parsed.Type = "interact"
		parsed.Interaction = "open_container"
		parsed.DiceExpr = "1d20"
		parsed.TargetQuery = extractEntity(trimmed, lower, []string{"\u6253\u5f00", "\u5f00\u542f", "open", "unlock"})
		parsed.TargetQuery = normalizeContainerQuery(parsed.TargetQuery, lower)
	} else if hasAny(lower, "\u6253\u5f00", "\u5f00\u542f", "open", "unlock") {
		parsed.Type = "interact"
		parsed.Interaction = ""
		parsed.TargetQuery = extractEntity(trimmed, lower, []string{"\u6253\u5f00", "\u5f00\u542f", "open", "unlock"})
	}
	if hasAny(lower, "\u62fe\u53d6", "\u6349\u8d77", "\u83b7\u53d6", "pick up", "pickup", "take", "loot") {
		parsed.Type = "interact"
		parsed.Interaction = "pickup_item"
		parsed.TargetQuery = extractEntity(trimmed, lower, []string{"\u62fe\u53d6", "\u6349\u8d77", "\u83b7\u53d6", "pick up", "pickup", "take", "loot"})
	}
	if hasAny(lower, "\u4e22\u5f03", "\u6254\u6389", "\u4e22\u4e0b", "drop", "discard", "throw away") {
		parsed.Type = "interact"
		parsed.Interaction = "drop_item"
		parsed.TargetQuery = extractEntity(trimmed, lower, []string{"\u4e22\u5f03", "\u6254\u6389", "\u4e22\u4e0b", "drop", "discard", "throw away"})
	}

	if hasAny(lower, "\u8fd4\u56de", "\u540e\u9000", "back", "return") {
		parsed.Type = "pop_layer"
	}
	if hasAny(lower, "\u8fdb\u5165", "\u8d70\u8fdb", "\u8d70\u5165", "\u524d\u5f80", "\u53bb", "enter", "go to", "goto") {
		if layerID := extractEntity(trimmed, lower, []string{"\u8fdb\u5165", "\u8d70\u8fdb", "\u8d70\u5165", "\u524d\u5f80", "\u53bb", "enter", "go to", "goto"}); layerID != "" {
			parsed.Type = "push_layer"
			parsed.LayerID = normalizeLayerInput(layerID)
		}
	}
	if hasAny(lower, "\u5207\u6362", "\u66ff\u6362", "switch", "replace") {
		if layerID := extractEntity(trimmed, lower, []string{"\u5207\u6362", "\u66ff\u6362", "switch", "replace"}); layerID != "" {
			parsed.Type = "replace_layer"
			parsed.LayerID = normalizeLayerInput(layerID)
		}
	}

	if hasAny(lower, kwAdvantageCN, "advantage") {
		parsed.UseAdvantage = true
	}
	if hasAny(lower, kwDisadvantageCN, "disadvantage") {
		parsed.UseDisadvantage = true
	}

	return parsed, nil
}

func normalizeLayerInput(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, " ,.!?\uFF0C\u3002\uFF01\uFF1F")
	return raw
}

func (h *HTTPReferee) Call(input string) (ParsedAction, error) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if trimmed == "" {
		return ParsedAction{}, fmt.Errorf("input is required")
	}
	body, err := json.Marshal(map[string]any{"input": trimmed})
	if err != nil {
		return ParsedAction{}, fmt.Errorf("marshal request failed: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return ParsedAction{}, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("call ai referee failed: %w", err)
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_unavailable"] = err.Error()
		return fallback, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(resp.Body)
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("ai referee returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_status_error"] = fmt.Sprintf("status=%d", resp.StatusCode)
		return fallback, nil
	}
	var parsed ParsedAction
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("decode ai referee response failed: %w", err)
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_parse_error"] = err.Error()
		return fallback, nil
	}
	if strings.TrimSpace(parsed.Type) == "" {
		return ParsedAction{}, fmt.Errorf("ai referee response missing type")
	}
	return parsed, nil
}

func NewOpenAICompatibleReferee(rawURL, token, model string, timeout time.Duration, systemPrompt string) (*OpenAICompatibleReferee, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("ai referee url is required")
	}
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return nil, fmt.Errorf("invalid ai referee url: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("ai referee model is required")
	}
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = defaultSystemPrompt
	}
	return &OpenAICompatibleReferee{
		url:          rawURL,
		token:        token,
		model:        model,
		systemPrompt: systemPrompt,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (o *OpenAICompatibleReferee) Call(input string) (ParsedAction, error) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if trimmed == "" {
		return ParsedAction{}, fmt.Errorf("input is required")
	}
	reqPayload := map[string]any{
		"model": o.model,
		"messages": []map[string]string{
			{"role": "system", "content": o.systemPrompt},
			{"role": "user", "content": trimmed},
		},
		"temperature": 0.1,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "trpg_action",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"type": map[string]any{
							"type": "string",
							"enum": []string{"interact", "observe", "push_layer", "pop_layer", "replace_layer", "narrate"},
						},
						"interaction":      map[string]any{"type": "string"},
						"target_query":     map[string]any{"type": "string"},
						"layer_id":         map[string]any{"type": "string"},
						"dice_expr":        map[string]any{"type": "string"},
						"use_advantage":    map[string]any{"type": "boolean"},
						"use_disadvantage": map[string]any{"type": "boolean"},
						"patch":            map[string]any{"type": "object", "additionalProperties": true},
						"payload":          map[string]any{"type": "object", "additionalProperties": true},
						"create_object":    map[string]any{"type": "object", "additionalProperties": true},
					},
					"required": []string{"type"},
				},
			},
		},
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return ParsedAction{}, fmt.Errorf("marshal request failed: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return ParsedAction{}, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("call ai referee failed: %w", err)
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_unavailable"] = err.Error()
		return fallback, nil
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("ai referee returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_status_error"] = fmt.Sprintf("status=%d", resp.StatusCode)
		return fallback, nil
	}

	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &wrapper); err != nil {
		return ParsedAction{}, fmt.Errorf("decode ai response wrapper failed: %w", err)
	}
	if len(wrapper.Choices) == 0 {
		return ParsedAction{}, fmt.Errorf("ai response has no choices")
	}
	content := strings.TrimSpace(wrapper.Choices[0].Message.Content)
	if content == "" {
		return NewRuleReferee().Call(trimmed)
	}
	content = extractJSONObject(content)
	parsed, err := decodeParsedActionLoose([]byte(content))
	if err != nil {
		fallback, fallbackErr := NewRuleReferee().Call(trimmed)
		if fallbackErr != nil {
			return ParsedAction{}, fmt.Errorf("decode parsed action failed: %w", err)
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_parse_error"] = err.Error()
		return fallback, nil
	}
	parsed = normalizeParsedAction(trimmed, parsed)
	if !isAllowedActionType(parsed.Type) {
		fallback, _ := NewRuleReferee().Call(trimmed)
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_invalid_type"] = parsed.Type
		parsed = fallback
	}
	if strings.TrimSpace(parsed.Type) == "" {
		return ParsedAction{}, fmt.Errorf("ai parsed action missing type")
	}
	if parsed.Payload == nil {
		parsed.Payload = map[string]any{}
	}
	parsed.Payload["raw_input"] = trimmed
	return parsed, nil
}

func isAllowedActionType(actionType string) bool {
	switch actionType {
	case "interact", "observe", "push_layer", "pop_layer", "replace_layer", "narrate":
		return true
	default:
		return false
	}
}

func extractJSONObject(content string) string {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return content
}

func decodeParsedActionLoose(raw []byte) (ParsedAction, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ParsedAction{}, err
	}
	parsed := ParsedAction{
		Type:            asString(obj["type"]),
		Interaction:     asString(obj["interaction"]),
		TargetQuery:     asString(obj["target_query"]),
		LayerID:         asString(obj["layer_id"]),
		DiceExpr:        asString(obj["dice_expr"]),
		UseAdvantage:    asBool(obj["use_advantage"]),
		UseDisadvantage: asBool(obj["use_disadvantage"]),
		Patch:           asMap(obj["patch"]),
		Payload:         asMap(obj["payload"]),
	}
	if co := asMap(obj["create_object"]); len(co) > 0 {
		parsed.CreateObject = &ParsedObject{
			Name:  asString(co["name"]),
			Tags:  asStringSlice(co["tags"]),
			State: asMap(co["state"]),
		}
	}
	return parsed, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func asMap(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	// Make non-object payloads/paches still representable instead of failing hard.
	return map[string]any{"value": v}
}

func asStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func normalizeParsedAction(rawInput string, parsed ParsedAction) ParsedAction {
	// Repair weak LLM outputs with deterministic rules.
	ruleParsed, _ := NewRuleReferee().Call(rawInput)
	low := strings.ToLower(strings.TrimSpace(rawInput))
	if ruleParsed.Type == "interact" && ruleParsed.Interaction == "open_container" {
		if parsed.Type != "interact" || parsed.Interaction == "" {
			parsed.Type = "interact"
			parsed.Interaction = "open_container"
		}
		parsed.TargetQuery = ruleParsed.TargetQuery
		parsed.DiceExpr = ""
	}
	if ruleParsed.Type == "interact" && (ruleParsed.Interaction == "pickup_item" || ruleParsed.Interaction == "drop_item") {
		parsed.Type = "interact"
		parsed.Interaction = ruleParsed.Interaction
		if parsed.TargetQuery == "" {
			parsed.TargetQuery = ruleParsed.TargetQuery
		}
	}

	if strings.TrimSpace(parsed.Type) == "" || parsed.Type == "narrate" {
		if ruleParsed.Type != "" && ruleParsed.Type != "narrate" {
			parsed.Type = ruleParsed.Type
		}
	}
	if parsed.LayerID == "" && (parsed.Type == "push_layer" || parsed.Type == "replace_layer") {
		parsed.LayerID = ruleParsed.LayerID
	}
	if parsed.Type == "push_layer" || parsed.Type == "replace_layer" {
		// For scene transitions, prefer user-facing natural layer names over synthetic slugs like hall_main.
		if ruleLayer := strings.TrimSpace(ruleParsed.LayerID); ruleLayer != "" {
			if parsed.LayerID == "" || isSyntheticLayerID(parsed.LayerID) || (hasChinese(ruleLayer) && !hasChinese(parsed.LayerID)) {
				parsed.LayerID = ruleLayer
			}
		}
		if parsed.LayerID == "" {
			if tq := strings.TrimSpace(parsed.TargetQuery); tq != "" && !isSyntheticLayerID(tq) {
				parsed.LayerID = tq
			}
		}
	}
	parsed.LayerID = normalizeLayerInput(parsed.LayerID)
	if parsed.Type == "narrate" && parsed.LayerID != "" {
		switch {
		case hasAny(low, "\u8fd4\u56de", "\u540e\u9000", "back", "return"):
			parsed.Type = "pop_layer"
		case hasAny(low, "\u5207\u6362", "\u66ff\u6362", "switch", "replace"):
			parsed.Type = "replace_layer"
		case hasAny(low, "\u8fdb\u5165", "\u8d70\u8fdb", "\u8d70\u5165", "\u524d\u5f80", "\u53bb", "enter", "go to", "goto"):
			parsed.Type = "push_layer"
		}
	}
	if parsed.TargetQuery == "" && parsed.Type == "interact" {
		parsed.TargetQuery = ruleParsed.TargetQuery
	}
	if parsed.TargetQuery == "" && parsed.Type == "observe" {
		parsed.TargetQuery = ruleParsed.TargetQuery
	}
	if parsed.Type == "interact" && parsed.Interaction == "" {
		parsed.Interaction = ruleParsed.Interaction
	}
	if parsed.Type == "interact" && parsed.Interaction == "open_container" {
		query := strings.TrimSpace(parsed.TargetQuery)
		if query == "" {
			query = strings.TrimSpace(ruleParsed.TargetQuery)
		}
		if !looksLikeContainerQuery(query, low) {
			parsed.Type = "interact"
			parsed.Interaction = ""
			if parsed.TargetQuery == "" {
				parsed.TargetQuery = query
			}
		}
	}
	if parsed.DiceExpr == "" && parsed.Type == "interact" && parsed.Interaction == "open_container" {
		parsed.DiceExpr = "1d20"
	}
	if parsed.Payload == nil {
		parsed.Payload = map[string]any{}
	}
	return parsed
}

func normalizeContainerQuery(query, lowerInput string) string {
	q := strings.TrimSpace(query)
	q = strings.TrimLeft(q, "\u4e86\u7740\u628a\u5c06\u4e00\u4e0b\u5730")
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		if hasAny(lowerInput, "chest", "box", "crate") {
			return "chest"
		}
		return "\u7bb1\u5b50"
	case strings.Contains(q, "\u7bb1"):
		return "\u7bb1\u5b50"
	case strings.Contains(strings.ToLower(q), "chest"), strings.Contains(strings.ToLower(q), "box"), strings.Contains(strings.ToLower(q), "crate"):
		return "chest"
	default:
		return q
	}
}

func hasAny(input string, keys ...string) bool {
	for _, key := range keys {
		if strings.Contains(input, key) {
			return true
		}
	}
	return false
}

func extractEntity(raw, lower string, keywords []string) string {
	for _, keyword := range keywords {
		idx := strings.Index(lower, keyword)
		if idx < 0 {
			continue
		}
		start := idx + len(keyword)
		if start >= len(raw) {
			return ""
		}
		candidate := strings.TrimSpace(raw[start:])
		candidate = strings.Trim(candidate, " ,.!?\uFF0C\u3002\uFF01\uFF1F")
		candidate = strings.TrimPrefix(candidate, "the ")
		candidate = strings.TrimPrefix(candidate, "\u4e00\u4e2a")
		candidate = strings.TrimPrefix(candidate, "\u4e00\u53ea")
		candidate = strings.TrimPrefix(candidate, "\u4e00\u540d")
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func looksLikeContainerQuery(query, lowerInput string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return hasAny(lowerInput, "箱", "柜", "抽屉", "容器", "盒", "chest", "box", "crate", "cabinet", "drawer", "locker", "container")
	}
	return hasAny(q, "箱", "柜", "抽屉", "容器", "盒", "chest", "box", "crate", "cabinet", "drawer", "locker", "container")
}

func isSyntheticLayerID(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "scene-") || strings.HasPrefix(v, "scene_") {
		return true
	}
	hasOnlySlugChars := true
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		hasOnlySlugChars = false
		break
	}
	return hasOnlySlugChars
}

func hasChinese(v string) bool {
	for _, r := range v {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isSurroundingsQuery(query string) bool {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return false
	}
	switch q {
	case "四周", "周围", "附近", "周遭", "周边", "四处", "环境", "四处看看", "周围环境", "周围看看", "周围情况", "四周情况",
		"surroundings", "around", "nearby", "area", "environment":
		return true
	default:
		return false
	}
}
