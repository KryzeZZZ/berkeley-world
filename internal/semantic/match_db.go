//go:build pgvector

package semantic

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// MatchCandidatesWithDB matches using embeddings table in PostgreSQL.
func MatchCandidatesWithDB(ctx context.Context, entityType, query string, candidates map[string]string) (MatchResult, error) {
	entityType = strings.TrimSpace(entityType)
	query = strings.TrimSpace(query)
	if entityType == "" {
		return MatchResult{}, fmt.Errorf("entity_type is required")
	}
	if query == "" {
		return MatchResult{}, fmt.Errorf("query is required")
	}
	if len(candidates) == 0 {
		return MatchResult{}, fmt.Errorf("candidates are required")
	}
	dsn := strings.TrimSpace(os.Getenv("EMBEDDINGS_PG_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("WORLD_PG_DSN"))
	}
	if dsn == "" {
		return MatchResult{}, fmt.Errorf("EMBEDDINGS_PG_DSN is required")
	}

	cfgPath := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_CONFIG"))
	if cfgPath == "" {
		cfgPath = "config/layer_semantic.json"
	}
	semCfg, _ := LoadConfigFromFile(cfgPath)
	minScore := semCfg.MinScore
	minGap := semCfg.MinGap
	if raw := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_MIN_SCORE")); raw != "" {
		if parsed, err := parseFloat(raw); err == nil {
			minScore = parsed
		}
	}
	if raw := strings.TrimSpace(os.Getenv("LAYER_SEMANTIC_MIN_GAP")); raw != "" {
		if parsed, err := parseFloat(raw); err == nil {
			minGap = parsed
		}
	}
	if minScore <= 0 {
		minScore = 0.40
	}
	if minGap < 0 {
		minGap = 0.04
	}

	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return MatchResult{}, fmt.Errorf("connect embeddings db failed: %w", err)
	}
	defer pool.Close()

	if err := ensureEmbeddings(ctx, pool, entityType, candidates); err != nil {
		return MatchResult{}, err
	}

	vecs, err := EmbedTexts(ctx, []string{query})
	if err != nil {
		return MatchResult{}, err
	}
	queryVec := vecs[0]
	if len(queryVec) == 0 {
		return MatchResult{}, fmt.Errorf("query embedding is empty")
	}

	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	rows, err := pool.Query(ctx,
		"SELECT entity_id, 1 - (embedding <=> $1) AS score FROM embeddings WHERE entity_type=$2 AND entity_id = ANY($3) ORDER BY embedding <=> $1 LIMIT 2",
		pgvector.NewVector(float64ToFloat32(queryVec)),
		entityType,
		ids,
	)
	if err != nil {
		return MatchResult{}, fmt.Errorf("query embeddings failed: %w", err)
	}
	defer rows.Close()

	type row struct {
		id    string
		score float64
	}
	results := make([]row, 0, 2)
	for rows.Next() {
		var id string
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			return MatchResult{}, fmt.Errorf("scan embeddings failed: %w", err)
		}
		results = append(results, row{id: id, score: score})
	}
	if len(results) == 0 {
		return MatchResult{}, fmt.Errorf("no semantic candidates")
	}
	if results[0].score < minScore {
		return MatchResult{}, fmt.Errorf("semantic match score too low: %.3f", results[0].score)
	}
	if len(results) > 1 && (results[0].score-results[1].score) < minGap {
		return MatchResult{}, fmt.Errorf("semantic match ambiguous: %.3f vs %.3f", results[0].score, results[1].score)
	}
	return MatchResult{CandidateID: results[0].id, Score: results[0].score}, nil
}

func ensureEmbeddings(ctx context.Context, pool *pgxpool.Pool, entityType string, candidates map[string]string) error {
	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, "SELECT entity_id, text FROM embeddings WHERE entity_type=$1 AND entity_id = ANY($2)", entityType, ids)
	if err != nil {
		return fmt.Errorf("query embeddings failed: %w", err)
	}
	defer rows.Close()
	existing := map[string]string{}
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			return fmt.Errorf("scan embeddings failed: %w", err)
		}
		existing[id] = text
	}

	toEmbed := make([]string, 0)
	toEmbedIDs := make([]string, 0)
	for id, text := range candidates {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if existing[id] == text {
			continue
		}
		toEmbedIDs = append(toEmbedIDs, id)
		toEmbed = append(toEmbed, text)
	}
	if len(toEmbed) == 0 {
		return nil
	}

	vectors, err := EmbedTexts(ctx, toEmbed)
	if err != nil {
		return err
	}
	if len(vectors) != len(toEmbedIDs) {
		return fmt.Errorf("embedding size mismatch")
	}
	batch := &pgx.Batch{}
	for i, id := range toEmbedIDs {
		batch.Queue(
			"INSERT INTO embeddings (entity_type, entity_id, text, embedding, updated_at) VALUES ($1, $2, $3, $4, NOW()) ON CONFLICT (entity_type, entity_id) DO UPDATE SET text=EXCLUDED.text, embedding=EXCLUDED.embedding, updated_at=NOW()",
			entityType,
			id,
			toEmbed[i],
			pgvector.NewVector(float64ToFloat32(vectors[i])),
		)
	}
	results := pool.SendBatch(ctx, batch)
	defer results.Close()
	for range toEmbedIDs {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("upsert embeddings failed: %w", err)
		}
	}
	return nil
}

func parseFloat(v string) (float64, error) {
	return strconv.ParseFloat(v, 64)
}
