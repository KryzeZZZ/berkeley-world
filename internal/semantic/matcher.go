package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MatchResult struct {
	CandidateID string
	Score       float64
}

type Matcher struct {
	url      string
	model    string
	token    string
	client   *http.Client
	minScore float64
	minGap   float64
	store    embeddingStore
	dim      int
}

var (
	defaultMatcherOnce sync.Once
	defaultMatcher     *Matcher
	defaultMatcherErr  error
)

func DefaultMatcher() (*Matcher, error) {
	defaultMatcherOnce.Do(func() {
		defaultMatcher, defaultMatcherErr = NewMatcherFromEnv()
	})
	return defaultMatcher, defaultMatcherErr
}

func NewMatcherFromEnv() (*Matcher, error) {
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
		refereeURL := strings.TrimSpace(os.Getenv("AI_REFEREE_URL"))
		rawURL = strings.Replace(refereeURL, "/chat/completions", "/embeddings", 1)
	}
	if rawURL == "" {
		return nil, fmt.Errorf("semantic matcher unavailable: missing LAYER_EMBEDDING_URL")
	}
	model := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_MODEL"))
	if model == "" {
		model = cfg.Model
	}
	token := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_TOKEN"))
	if token == "" {
		token = cfg.Token
	}
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AI_REFEREE_TOKEN"))
	}
	timeout := cfg.Timeout
	if rawTimeout := strings.TrimSpace(os.Getenv("LAYER_EMBEDDING_TIMEOUT")); rawTimeout != "" {
		parsed, err := time.ParseDuration(rawTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid LAYER_EMBEDDING_TIMEOUT: %w", err)
		}
		timeout = parsed
	}
	minScore := cfg.MinScore
	if rawScore := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_MIN_SCORE")); rawScore != "" {
		parsed, err := strconv.ParseFloat(rawScore, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid LAYER_SEMANTIC_MIN_SCORE: %w", err)
		}
		minScore = parsed
	}
	minGap := cfg.MinGap
	if rawGap := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_MIN_GAP")); rawGap != "" {
		parsed, err := strconv.ParseFloat(rawGap, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid LAYER_SEMANTIC_MIN_GAP: %w", err)
		}
		minGap = parsed
	}

	var store embeddingStore
	dim := 768
	if rawDim := strings.TrimSpace(os.Getenv("SEMANTIC_EMBEDDING_DIM")); rawDim != "" {
		if parsed, err := strconv.Atoi(rawDim); err == nil && parsed > 0 {
			dim = parsed
		}
	}
	if dsn := strings.TrimSpace(os.Getenv("SEMANTIC_PG_DSN")); dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		pgStore, err := newPGEmbeddingStore(ctx, dsn, dim)
		if err != nil {
			return nil, err
		}
		store = pgStore
	}

	return &Matcher{
		url:      rawURL,
		model:    model,
		token:    token,
		client:   &http.Client{Timeout: timeout},
		minScore: minScore,
		minGap:   minGap,
		store:    store,
		dim:      dim,
	}, nil
}

func (m *Matcher) MatchLayer(ctx context.Context, query string, candidates map[string][]string) (MatchResult, error) {
	return m.MatchCandidates(ctx, query, candidates)
}

func (m *Matcher) MatchCandidates(ctx context.Context, query string, candidates map[string][]string) (MatchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return MatchResult{}, fmt.Errorf("query is required")
	}
	if len(candidates) == 0 {
		return MatchResult{}, fmt.Errorf("candidates are required")
	}

	orderedIDs := make([]string, 0, len(candidates))
	texts := make([]string, 0, 1+len(candidates)*2)
	queryIndex := 0
	texts = append(texts, query)

	candidateTextRanges := make(map[string][2]int, len(candidates))
	for id, phrases := range candidates {
		orderedIDs = append(orderedIDs, id)
		start := len(texts)
		for _, phrase := range phrases {
			phrase = strings.TrimSpace(phrase)
			if phrase == "" {
				continue
			}
			texts = append(texts, phrase)
		}
		end := len(texts)
		candidateTextRanges[id] = [2]int{start, end}
	}
	sort.Strings(orderedIDs)

	vectors, err := m.embed(ctx, texts)
	if err != nil {
		return MatchResult{}, err
	}
	if len(vectors) != len(texts) {
		return MatchResult{}, fmt.Errorf("embedding size mismatch")
	}
	queryVector := vectors[queryIndex]

	results := make([]MatchResult, 0, len(candidates))
	for _, id := range orderedIDs {
		rng := candidateTextRanges[id]
		best := -1.0
		for i := rng[0]; i < rng[1]; i++ {
			score := cosineSimilarity(queryVector, vectors[i])
			if score > best {
				best = score
			}
		}
		if best < 0 {
			best = 0
		}
		results = append(results, MatchResult{CandidateID: id, Score: best})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) == 0 {
		return MatchResult{}, fmt.Errorf("no semantic candidates")
	}
	top := results[0]
	if top.Score < m.minScore {
		return MatchResult{}, fmt.Errorf("semantic match score too low: %.3f", top.Score)
	}
	if len(results) > 1 && (top.Score-results[1].Score) < m.minGap {
		return MatchResult{}, fmt.Errorf("semantic match ambiguous: %.3f vs %.3f", top.Score, results[1].Score)
	}
	return top, nil
}

func (m *Matcher) embed(ctx context.Context, inputs []string) ([][]float64, error) {
	if m.store == nil {
		return m.embedRemote(ctx, inputs)
	}
	unique := make([]string, 0, len(inputs))
	seen := map[string]struct{}{}
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if _, ok := seen[input]; ok {
			continue
		}
		seen[input] = struct{}{}
		unique = append(unique, input)
	}
	cache, err := m.store.GetEmbeddings(ctx, unique)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0)
	for _, input := range unique {
		if input == "" {
			continue
		}
		if _, ok := cache[input]; !ok {
			missing = append(missing, input)
		}
	}
	if len(missing) > 0 {
		vectors, err := m.embedRemote(ctx, missing)
		if err != nil {
			return nil, err
		}
		newVectors := make(map[string][]float64, len(missing))
		for i, text := range missing {
			newVectors[text] = vectors[i]
		}
		if err := m.store.UpsertEmbeddings(ctx, newVectors); err != nil {
			return nil, err
		}
		for key, vec := range newVectors {
			cache[key] = vec
		}
	}

	ordered := make([][]float64, len(inputs))
	for i, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			ordered[i] = nil
			continue
		}
		ordered[i] = cache[input]
	}
	return ordered, nil
}

func (m *Matcher) embedRemote(ctx context.Context, inputs []string) ([][]float64, error) {
	payload := map[string]any{
		"model": m.model,
		"input": inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding API status %d", resp.StatusCode)
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

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
