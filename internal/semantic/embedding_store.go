//go:build pgvector

package semantic

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type pgEmbeddingStore struct {
	pool *pgxpool.Pool
	dim  int
}

func newPGEmbeddingStore(ctx context.Context, dsn string, dim int) (*pgEmbeddingStore, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("pgvector dsn is required")
	}
	if dim <= 0 {
		return nil, fmt.Errorf("embedding dimension is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pgvector dsn failed: %w", err)
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect pgvector failed: %w", err)
	}
	store := &pgEmbeddingStore{pool: pool, dim: dim}
	if err := store.init(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *pgEmbeddingStore) init(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("enable pgvector failed: %w", err)
	}
	createTable := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS semantic_embedding_cache (text TEXT PRIMARY KEY, embedding vector(%d) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		s.dim,
	)
	if _, err := s.pool.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("create embedding cache table failed: %w", err)
	}
	indexSQL := "CREATE INDEX IF NOT EXISTS semantic_embedding_cache_embedding_idx ON semantic_embedding_cache USING ivfflat (embedding vector_cosine_ops)"
	if _, err := s.pool.Exec(ctx, indexSQL); err != nil {
		return fmt.Errorf("create embedding index failed: %w", err)
	}
	return nil
}

func (s *pgEmbeddingStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *pgEmbeddingStore) GetEmbeddings(ctx context.Context, texts []string) (map[string][]float64, error) {
	out := make(map[string][]float64, len(texts))
	if len(texts) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT text, embedding FROM semantic_embedding_cache WHERE text = ANY($1)", texts)
	if err != nil {
		return nil, fmt.Errorf("query embedding cache failed: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var text string
		var vec pgvector.Vector
		if err := rows.Scan(&text, &vec); err != nil {
			return nil, fmt.Errorf("scan embedding cache failed: %w", err)
		}
		out[text] = vectorToFloat64(vec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding cache failed: %w", err)
	}
	return out, nil
}

func (s *pgEmbeddingStore) UpsertEmbeddings(ctx context.Context, vectors map[string][]float64) error {
	if len(vectors) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	batch := &pgx.Batch{}
	for text, vec := range vectors {
		if strings.TrimSpace(text) == "" {
			continue
		}
		if len(vec) != s.dim {
			return fmt.Errorf("embedding dimension mismatch for %s: %d", text, len(vec))
		}
		batch.Queue(
			"INSERT INTO semantic_embedding_cache (text, embedding, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (text) DO UPDATE SET embedding = EXCLUDED.embedding, updated_at = NOW()",
			text,
			pgvector.NewVector(float64ToFloat32(vec)),
		)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range vectors {
		if _, err := results.Exec(); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("upsert embedding cache failed: %w", err)
		}
	}
	return nil
}

func vectorToFloat64(v pgvector.Vector) []float64 {
	values := v.Slice()
	if len(values) == 0 {
		return nil
	}
	out := make([]float64, len(values))
	for i, val := range values {
		out[i] = float64(val)
	}
	return out
}

func float64ToFloat32(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, v := range values {
		out[i] = float32(v)
	}
	return out
}
