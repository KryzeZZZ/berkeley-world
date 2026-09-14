//go:build pgvector

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"SAO/internal/config"
	"SAO/internal/model"
	"SAO/internal/semantic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type persistedWorld struct {
	PlayerToScene map[string]string `json:"player_to_scene"`
	Scenes        []sceneSnapshot   `json:"scenes"`
}

type sceneSnapshot struct {
	ID         string               `json:"id"`
	State      model.SceneState     `json:"state"`
	LayerStack []string             `json:"layer_stack"`
	Players    []*model.PlayerState `json:"players"`
	Objects    []*model.GameObject  `json:"objects"`
}

func main() {
	_ = config.LoadEnvFile(envFilePath())

	dsn := strings.TrimSpace(os.Getenv("WORLD_PG_DSN"))
	if dsn == "" {
		fmt.Println("WORLD_PG_DSN is required")
		os.Exit(1)
	}
	worldID := strings.TrimSpace(os.Getenv("WORLD_ID"))
	if worldID == "" {
		worldID = "default"
	}
	path := strings.TrimSpace(os.Getenv("WORLD_JSON_PATH"))
	if path == "" {
		path = "data/world.json"
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("read world json failed: %v\n", err)
		os.Exit(1)
	}
	raw = stripUTF8BOM(raw)
	var worldData persistedWorld
	if err := json.Unmarshal(raw, &worldData); err != nil {
		fmt.Printf("parse world json failed: %v\n", err)
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

	if err := importWorld(ctx, pool, worldID, worldData); err != nil {
		fmt.Printf("import world failed: %v\n", err)
		os.Exit(1)
	}
	if err := upsertEmbeddings(ctx, pool, worldData); err != nil {
		fmt.Printf("embedding upsert failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("world imported into tables (world_id=%s)\n", worldID)
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func importWorld(ctx context.Context, pool *pgxpool.Pool, worldID string, worldData persistedWorld) error {
	worldID = strings.TrimSpace(worldID)
	if worldID == "" {
		worldID = "default"
	}
	_, err := pool.Exec(ctx, "INSERT INTO worlds (id, name, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (id) DO UPDATE SET updated_at=NOW()", worldID, worldID)
	if err != nil {
		return fmt.Errorf("upsert world failed: %w", err)
	}

	for _, snap := range worldData.Scenes {
		if err := importScene(ctx, pool, worldID, snap); err != nil {
			return err
		}
	}
	return nil
}

func importScene(ctx context.Context, pool *pgxpool.Pool, worldID string, snap sceneSnapshot) error {
	sceneID := strings.TrimSpace(snap.ID)
	if sceneID == "" {
		return fmt.Errorf("scene id is required")
	}
	snap.Objects = mergeObjectsFromLayerItemStates(snap)
	ensurePlayerAttributesMeta(&snap)
	metaJSON := []byte("{}")
	if snap.State.Meta != nil {
		if raw, err := json.Marshal(snap.State.Meta); err == nil {
			metaJSON = raw
		}
	}
	_, err := pool.Exec(ctx, "INSERT INTO scenes (id, world_id, name, version, meta, updated_at) VALUES ($1, $2, $3, $4, $5, NOW()) ON CONFLICT (id) DO UPDATE SET world_id=EXCLUDED.world_id, name=EXCLUDED.name, version=EXCLUDED.version, meta=EXCLUDED.meta, updated_at=NOW()",
		sceneID, worldID, sceneID, snap.State.Version, metaJSON)
	if err != nil {
		return fmt.Errorf("upsert scene failed: %w", err)
	}

	layerIDs := collectLayerIDs(snap)
	layerDescs := asStringMap(snap.State.Meta, "layer_descriptions")
	layerBoot := asBoolMap(snap.State.Meta, "layer_bootstrapped")
	for _, layerID := range layerIDs {
		desc := strings.TrimSpace(layerDescs[layerID])
		boot := layerBoot[layerID]
		_, err := pool.Exec(ctx, "INSERT INTO layers (id, scene_id, name, description, bootstrapped, updated_at) VALUES ($1, $2, $3, $4, $5, NOW()) ON CONFLICT (id) DO UPDATE SET scene_id=EXCLUDED.scene_id, name=EXCLUDED.name, description=EXCLUDED.description, bootstrapped=EXCLUDED.bootstrapped, updated_at=NOW()",
			layerID, sceneID, layerID, desc, boot)
		if err != nil {
			return fmt.Errorf("upsert layer %s failed: %w", layerID, err)
		}
	}

	layerParents := asStringMap(snap.State.Meta, "layer_parents")
	for child, parent := range layerParents {
		if strings.TrimSpace(child) == "" || strings.TrimSpace(parent) == "" {
			continue
		}
		_, err := pool.Exec(ctx, "INSERT INTO layer_relations (parent_layer_id, child_layer_id, relation) VALUES ($1, $2, $3) ON CONFLICT (parent_layer_id, child_layer_id) DO UPDATE SET relation=EXCLUDED.relation",
			parent, child, "child")
		if err != nil {
			return fmt.Errorf("upsert layer relation failed: %w", err)
		}
	}

	for _, player := range snap.Players {
		if player == nil || strings.TrimSpace(player.ID) == "" {
			continue
		}
		_, err := pool.Exec(ctx, "INSERT INTO players (id, scene_id, name, updated_at) VALUES ($1, $2, $3, NOW()) ON CONFLICT (id) DO UPDATE SET scene_id=EXCLUDED.scene_id, name=EXCLUDED.name, updated_at=NOW()",
			player.ID, sceneID, player.Name)
		if err != nil {
			return fmt.Errorf("upsert player failed: %w", err)
		}
		for idx, layerID := range player.LayerStack {
			if strings.TrimSpace(layerID) == "" {
				continue
			}
			_, err := pool.Exec(ctx, "INSERT INTO player_layers (player_id, layer_id, pos) VALUES ($1, $2, $3) ON CONFLICT (player_id, layer_id) DO UPDATE SET pos=EXCLUDED.pos",
				player.ID, layerID, idx)
			if err != nil {
				return fmt.Errorf("upsert player layer failed: %w", err)
			}
		}
		for _, objID := range player.InventoryObjIDs {
			if strings.TrimSpace(objID) == "" {
				continue
			}
			_, err := pool.Exec(ctx, "INSERT INTO inventory (player_id, object_id) VALUES ($1, $2) ON CONFLICT (player_id, object_id) DO NOTHING",
				player.ID, objID)
			if err != nil {
				return fmt.Errorf("upsert inventory failed: %w", err)
			}
		}
	}

	for _, obj := range snap.Objects {
		if obj == nil || strings.TrimSpace(obj.ID) == "" {
			continue
		}
		stateJSON := []byte("{}")
		if obj.State != nil {
			if raw, err := json.Marshal(obj.State); err == nil {
				stateJSON = raw
			}
		}
		_, err := pool.Exec(ctx, "INSERT INTO objects (id, scene_id, name, state, version, updated_at) VALUES ($1, $2, $3, $4, $5, NOW()) ON CONFLICT (id) DO UPDATE SET scene_id=EXCLUDED.scene_id, name=EXCLUDED.name, state=EXCLUDED.state, version=EXCLUDED.version, updated_at=NOW()",
			obj.ID, sceneID, obj.Name, stateJSON, obj.Version)
		if err != nil {
			return fmt.Errorf("upsert object failed: %w", err)
		}
		for _, tag := range obj.Tags {
			if strings.TrimSpace(tag) == "" {
				continue
			}
			_, err := pool.Exec(ctx, "INSERT INTO object_tags (object_id, tag) VALUES ($1, $2) ON CONFLICT (object_id, tag) DO NOTHING",
				obj.ID, tag)
			if err != nil {
				return fmt.Errorf("upsert object tag failed: %w", err)
			}
		}
	}
	return nil
}

func ensurePlayerAttributesMeta(snap *sceneSnapshot) {
	if snap == nil {
		return
	}
	if snap.State.Meta == nil {
		snap.State.Meta = map[string]any{}
	}
	root := map[string]any{}
	for _, p := range snap.Players {
		if p == nil || strings.TrimSpace(p.ID) == "" {
			continue
		}
		panel := p.Attributes
		panel.Normalize()
		root[p.ID] = map[string]any{
			"strength":     panel.Strength,
			"dexterity":    panel.Dexterity,
			"constitution": panel.Constitution,
			"intelligence": panel.Intelligence,
			"wisdom":       panel.Wisdom,
			"charisma":     panel.Charisma,
			"luck":         panel.Luck,
		}
	}
	if len(root) > 0 {
		snap.State.Meta["player_attributes"] = root
	}
}

func upsertEmbeddings(ctx context.Context, pool *pgxpool.Pool, worldData persistedWorld) error {
	type entry struct {
		entityType string
		entityID   string
		text       string
	}
	entries := make([]entry, 0)
	for _, snap := range worldData.Scenes {
		objects := mergeObjectsFromLayerItemStates(snap)
		sceneText := buildSceneEmbeddingText(snap)
		if sceneText != "" {
			entries = append(entries, entry{entityType: "scene", entityID: snap.ID, text: sceneText})
		}
		layerDescs := asStringMap(snap.State.Meta, "layer_descriptions")
		for layerID, desc := range layerDescs {
			layerText := buildLayerEmbeddingText(layerID, desc)
			if layerText != "" {
				entries = append(entries, entry{entityType: "layer", entityID: layerID, text: layerText})
			}
		}
		for _, obj := range objects {
			if obj == nil {
				continue
			}
			objText := buildObjectEmbeddingText(obj)
			if objText != "" {
				entries = append(entries, entry{entityType: "object", entityID: obj.ID, text: objText})
			}
		}
	}
	if len(entries) == 0 {
		return nil
	}

	texts := make([]string, 0, len(entries))
	for _, e := range entries {
		texts = append(texts, e.text)
	}
	vectors, err := semantic.EmbedTexts(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed texts failed: %w", err)
	}
	if len(vectors) != len(entries) {
		return fmt.Errorf("embedding size mismatch")
	}
	batch := &pgx.Batch{}
	for i, e := range entries {
		batch.Queue(
			"INSERT INTO embeddings (entity_type, entity_id, text, embedding, updated_at) VALUES ($1, $2, $3, $4, NOW()) ON CONFLICT (entity_type, entity_id) DO UPDATE SET text=EXCLUDED.text, embedding=EXCLUDED.embedding, updated_at=NOW()",
			e.entityType,
			e.entityID,
			e.text,
			pgvector.NewVector(float64ToFloat32(vectors[i])),
		)
	}
	results := pool.SendBatch(ctx, batch)
	for range entries {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("upsert embeddings failed: %w", err)
		}
	}
	results.Close()
	return nil
}

func buildSceneEmbeddingText(snap sceneSnapshot) string {
	desc := ""
	if snap.State.Meta != nil {
		if m, ok := snap.State.Meta["layer_descriptions"].(map[string]any); ok {
			if v, ok := m[snap.ID].(string); ok {
				desc = v
			}
		}
		if desc == "" {
			if m, ok := snap.State.Meta["layer_descriptions"].(map[string]string); ok {
				desc = m[snap.ID]
			}
		}
	}
	desc = strings.TrimSpace(desc)
	if desc == "" {
		desc = snap.ID
	}
	return fmt.Sprintf("scene %s: %s", snap.ID, desc)
}

func buildLayerEmbeddingText(layerID, desc string) string {
	layerID = strings.TrimSpace(layerID)
	desc = strings.TrimSpace(desc)
	if layerID == "" && desc == "" {
		return ""
	}
	if desc == "" {
		desc = layerID
	}
	return fmt.Sprintf("layer %s: %s", layerID, desc)
}

func buildObjectEmbeddingText(obj *model.GameObject) string {
	if obj == nil {
		return ""
	}
	desc := ""
	if obj.State != nil {
		if v, ok := obj.State["description"].(string); ok {
			desc = v
		}
	}
	tags := strings.Join(obj.Tags, ", ")
	parts := []string{strings.TrimSpace(obj.Name)}
	if strings.TrimSpace(desc) != "" {
		parts = append(parts, strings.TrimSpace(desc))
	}
	if strings.TrimSpace(tags) != "" {
		parts = append(parts, strings.TrimSpace(tags))
	}
	return strings.Join(parts, " | ")
}

func collectLayerIDs(snap sceneSnapshot) []string {
	objects := mergeObjectsFromLayerItemStates(snap)
	set := map[string]struct{}{}
	if strings.TrimSpace(snap.ID) != "" {
		set[snap.ID] = struct{}{}
	}
	for _, layerID := range snap.LayerStack {
		if strings.TrimSpace(layerID) != "" {
			set[layerID] = struct{}{}
		}
	}
	for _, player := range snap.Players {
		if player == nil {
			continue
		}
		for _, layerID := range player.LayerStack {
			if strings.TrimSpace(layerID) != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		if layerID, ok := obj.State["layer"].(string); ok {
			if strings.TrimSpace(layerID) != "" {
				set[layerID] = struct{}{}
			}
		}
	}
	layerDescs := asStringMap(snap.State.Meta, "layer_descriptions")
	for layerID := range layerDescs {
		if strings.TrimSpace(layerID) != "" {
			set[layerID] = struct{}{}
		}
	}
	layerBoot := asBoolMap(snap.State.Meta, "layer_bootstrapped")
	for layerID := range layerBoot {
		if strings.TrimSpace(layerID) != "" {
			set[layerID] = struct{}{}
		}
	}
	layerParents := asStringMap(snap.State.Meta, "layer_parents")
	for child, parent := range layerParents {
		if strings.TrimSpace(child) != "" {
			set[child] = struct{}{}
		}
		if strings.TrimSpace(parent) != "" {
			set[parent] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for layerID := range set {
		out = append(out, layerID)
	}
	sort.Strings(out)
	return out
}

func asStringMap(meta map[string]any, key string) map[string]string {
	out := map[string]string{}
	if meta == nil {
		return out
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func asBoolMap(meta map[string]any, key string) map[string]bool {
	out := map[string]bool{}
	if meta == nil {
		return out
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case map[string]bool:
		for k, val := range v {
			out[k] = val
		}
	case map[string]any:
		for k, val := range v {
			if b, ok := val.(bool); ok {
				out[k] = b
			}
		}
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

func mergeObjectsFromLayerItemStates(snap sceneSnapshot) []*model.GameObject {
	merged := make([]*model.GameObject, 0, len(snap.Objects))
	seen := map[string]struct{}{}
	for _, obj := range snap.Objects {
		if obj == nil {
			continue
		}
		id := strings.TrimSpace(obj.ID)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, obj)
	}
	if snap.State.Meta == nil {
		return merged
	}
	raw, ok := snap.State.Meta["layer_item_states"]
	if !ok || raw == nil {
		return merged
	}
	layerStates, ok := raw.(map[string]any)
	if !ok {
		return merged
	}
	for _, itemsRaw := range layerStates {
		items, ok := itemsRaw.([]any)
		if !ok {
			continue
		}
		for _, itemRaw := range items {
			entry, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := entry["id"].(string)
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			name, _ := entry["name"].(string)
			state := map[string]any{}
			if stateRaw, ok := entry["state"].(map[string]any); ok {
				for k, v := range stateRaw {
					state[k] = v
				}
			}
			version := int64(1)
			switch typed := entry["version"].(type) {
			case int64:
				version = typed
			case int:
				version = int64(typed)
			case float64:
				version = int64(typed)
			}
			merged = append(merged, &model.GameObject{
				ID:      id,
				Name:    strings.TrimSpace(name),
				State:   state,
				Version: version,
			})
			seen[id] = struct{}{}
		}
	}
	return merged
}
