//go:build pgvector

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"SAO/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	_ = config.LoadEnvFile(envFilePath())

	dsn := strings.TrimSpace(os.Getenv("WORLD_PG_DSN"))
	if dsn == "" {
		fmt.Println("WORLD_PG_DSN is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Printf("connect db failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := migrate(ctx, pool); err != nil {
		fmt.Printf("migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migration completed")
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE TABLE IF NOT EXISTS worlds (id TEXT PRIMARY KEY, name TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE TABLE IF NOT EXISTS scenes (id TEXT PRIMARY KEY, world_id TEXT REFERENCES worlds(id) ON DELETE CASCADE, name TEXT, version INT NOT NULL DEFAULT 1, meta JSONB, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE TABLE IF NOT EXISTS layers (id TEXT PRIMARY KEY, scene_id TEXT REFERENCES scenes(id) ON DELETE CASCADE, name TEXT, description TEXT, bootstrapped BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE TABLE IF NOT EXISTS layer_relations (parent_layer_id TEXT REFERENCES layers(id) ON DELETE CASCADE, child_layer_id TEXT REFERENCES layers(id) ON DELETE CASCADE, relation TEXT NOT NULL DEFAULT 'child', PRIMARY KEY(parent_layer_id, child_layer_id))",
		"CREATE TABLE IF NOT EXISTS players (id TEXT PRIMARY KEY, scene_id TEXT REFERENCES scenes(id) ON DELETE SET NULL, name TEXT, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE TABLE IF NOT EXISTS player_layers (player_id TEXT REFERENCES players(id) ON DELETE CASCADE, layer_id TEXT REFERENCES layers(id) ON DELETE CASCADE, pos INT NOT NULL DEFAULT 0, PRIMARY KEY(player_id, layer_id))",
		"CREATE TABLE IF NOT EXISTS objects (id TEXT PRIMARY KEY, scene_id TEXT REFERENCES scenes(id) ON DELETE CASCADE, name TEXT NOT NULL, state JSONB, version INT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE TABLE IF NOT EXISTS object_tags (object_id TEXT REFERENCES objects(id) ON DELETE CASCADE, tag TEXT NOT NULL, PRIMARY KEY(object_id, tag))",
		"CREATE TABLE IF NOT EXISTS inventory (player_id TEXT REFERENCES players(id) ON DELETE CASCADE, object_id TEXT REFERENCES objects(id) ON DELETE CASCADE, PRIMARY KEY(player_id, object_id))",
		"CREATE TABLE IF NOT EXISTS embeddings (id BIGSERIAL PRIMARY KEY, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, text TEXT NOT NULL, embedding vector(768) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())",
		"CREATE INDEX IF NOT EXISTS embeddings_vec_idx ON embeddings USING ivfflat (embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS embeddings_entity_idx ON embeddings(entity_type, entity_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS embeddings_entity_unique ON embeddings(entity_type, entity_id)",
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec migration statement failed: %w", err)
		}
	}
	return nil
}
