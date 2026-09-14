package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OutcomeLayerPlan struct {
	LayerID     string `json:"layer_id"`
	ParentLayer string `json:"parent_layer,omitempty"`
	Relation    string `json:"relation,omitempty"` // child|sibling|parent|same
	Description string `json:"description,omitempty"`
}

type OutcomeItemLayerPlan struct {
	ItemName string `json:"item_name"`
	LayerID  string `json:"layer_id"`
}

type OutcomeLayerItemPlan struct {
	Layers []OutcomeLayerPlan     `json:"layers,omitempty"`
	Items  []OutcomeItemLayerPlan `json:"items,omitempty"`
}

func PlanOutcomeLayersAndItemsWithAI(req OutcomeRequest, narration, defaultLayer string) (OutcomeLayerItemPlan, error) {
	narration = strings.TrimSpace(narration)
	defaultLayer = strings.TrimSpace(defaultLayer)
	if narration == "" || defaultLayer == "" {
		return OutcomeLayerItemPlan{}, nil
	}
	cfg, err := loadOutcomeConfig()
	if err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	if cfg.Mode != "openai_compatible" || cfg.URL == "" || cfg.Model == "" {
		return OutcomeLayerItemPlan{}, nil
	}

	user := map[string]any{
		"default_layer":   defaultLayer,
		"narration":       narration,
		"context":         req.Context,
		"interaction":     req.Interaction,
		"instruction":     req.Instruction,
		"item_candidates": uniqueNonEmpty(append(append([]string{}, req.ContextKnownItemNames()...), ExtractItemNamesFromNarration(narration)...)),
	}
	userBytes, _ := json.Marshal(user)
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Plan TRPG scene structure from outcome narration. " +
					"Return JSON only with keys layers and items. " +
					"layers: list of {layer_id,parent_layer,relation,description}. " +
					"items: list of {item_name,layer_id}. " +
					"For scene_bootstrap, infer child layers under default_layer when narration contains sub-areas. " +
					"Only spatial/enterable area nouns can be layers (e.g., room, hall, corridor, floor). " +
					"Furniture/props cannot be layers; they must stay in items. " +
					"Assign each interactive item noun to the most specific layer. " +
					"Use relation enum strictly: child,sibling,parent,same. " +
					"Do not output vague names.",
			},
			{"role": "user", "content": string(userBytes)},
		},
		"temperature": 0.1,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "outcome_layer_item_plan",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"layers": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"layer_id":     map[string]any{"type": "string"},
									"parent_layer": map[string]any{"type": "string"},
									"relation":     map[string]any{"type": "string", "enum": []string{"child", "sibling", "parent", "same"}},
									"description":  map[string]any{"type": "string"},
								},
								"required": []string{"layer_id"},
							},
						},
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"item_name": map[string]any{"type": "string"},
									"layer_id":  map[string]any{"type": "string"},
								},
								"required": []string{"item_name", "layer_id"},
							},
						},
					},
					"required": []string{"layers", "items"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OutcomeLayerItemPlan{}, fmt.Errorf("layer-item planner status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	if len(wrapper.Choices) == 0 {
		return OutcomeLayerItemPlan{}, nil
	}
	content := extractJSONObject(strings.TrimSpace(wrapper.Choices[0].Message.Content))
	var out OutcomeLayerItemPlan
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return OutcomeLayerItemPlan{}, err
	}
	for i := range out.Layers {
		out.Layers[i].LayerID = strings.TrimSpace(out.Layers[i].LayerID)
		out.Layers[i].ParentLayer = strings.TrimSpace(out.Layers[i].ParentLayer)
		out.Layers[i].Relation = strings.ToLower(strings.TrimSpace(out.Layers[i].Relation))
		out.Layers[i].Description = strings.TrimSpace(out.Layers[i].Description)
	}
	for i := range out.Items {
		out.Items[i].ItemName = strings.TrimSpace(out.Items[i].ItemName)
		out.Items[i].LayerID = strings.TrimSpace(out.Items[i].LayerID)
	}
	return out, nil
}

func (req OutcomeRequest) ContextKnownItemNames() []string {
	if len(req.Context.KnownItems) == 0 {
		return nil
	}
	out := make([]string, 0, len(req.Context.KnownItems))
	for _, item := range req.Context.KnownItems {
		if n := strings.TrimSpace(item.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}
