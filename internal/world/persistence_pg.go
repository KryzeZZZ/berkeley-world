//go:build pgvector

package world

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func loadWorldFromDB(dsn, worldID string) (persistedWorld, bool, error) {
	worldID = normalizeWorldID(worldID)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return persistedWorld{}, false, fmt.Errorf("connect world db failed: %w", err)
	}
	defer pool.Close()
	if err := initWorldTable(ctx, pool); err != nil {
		return persistedWorld{}, false, err
	}
	var raw []byte
	err = pool.QueryRow(ctx, "SELECT data FROM sao_world_state WHERE world_id=$1", worldID).Scan(&raw)
	if err != nil {
		return persistedWorld{}, false, nil
	}
	var worldData persistedWorld
	if err := json.Unmarshal(raw, &worldData); err != nil {
		return persistedWorld{}, false, fmt.Errorf("decode world state failed: %w", err)
	}
	return worldData, true, nil
}

func persistWorldToDB(dsn, worldID string, worldData persistedWorld) error {
	payload, err := json.Marshal(worldData)
	if err != nil {
		return fmt.Errorf("marshal world state failed: %w", err)
	}
	return SaveWorldSnapshotToDB(dsn, worldID, payload)
}

func SaveWorldSnapshotToDB(dsn, worldID string, payload any) error {
	worldID = normalizeWorldID(worldID)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect world db failed: %w", err)
	}
	defer pool.Close()
	if err := initWorldTable(ctx, pool); err != nil {
		return err
	}
	var body []byte
	switch v := payload.(type) {
	case []byte:
		body = v
	case string:
		body = []byte(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal world payload failed: %w", err)
		}
		body = encoded
	}
	_, err = pool.Exec(ctx, "INSERT INTO sao_world_state (world_id, data, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (world_id) DO UPDATE SET data=EXCLUDED.data, updated_at=NOW()", worldID, body)
	if err != nil {
		return fmt.Errorf("upsert world state failed: %w", err)
	}
	return nil
}

func initWorldTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS sao_world_state (world_id TEXT PRIMARY KEY, data JSONB NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())")
	if err != nil {
		return fmt.Errorf("create world table failed: %w", err)
	}
	return nil
}

func normalizeWorldID(worldID string) string {
	worldID = strings.TrimSpace(worldID)
	if worldID == "" {
		return "default"
	}
	return worldID
}
