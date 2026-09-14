//go:build pgvector

package world

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"SAO/internal/model"
	"SAO/internal/semantic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

func loadWorldFromStructuredDB(dsn, worldID string) (persistedWorld, bool, error) {
	worldID = normalizeWorldID(worldID)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return persistedWorld{}, false, fmt.Errorf("connect world db failed: %w", err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, "SELECT id, version, meta FROM scenes WHERE world_id=$1", worldID)
	if err != nil {
		return persistedWorld{}, false, fmt.Errorf("query scenes failed: %w", err)
	}
	defer rows.Close()

	var scenes []sceneSnapshot
	for rows.Next() {
		var sceneID string
		var version int64
		var metaRaw []byte
		if err := rows.Scan(&sceneID, &version, &metaRaw); err != nil {
			return persistedWorld{}, false, fmt.Errorf("scan scene failed: %w", err)
		}
		meta := map[string]any{}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &meta)
		}
		scenes = append(scenes, sceneSnapshot{
			ID:    sceneID,
			State: model.SceneState{ID: sceneID, Version: version, Meta: meta},
		})
	}
	if err := rows.Err(); err != nil {
		return persistedWorld{}, false, fmt.Errorf("iterate scenes failed: %w", err)
	}
	if len(scenes) == 0 {
		return persistedWorld{}, false, nil
	}

	playerToScene := map[string]string{}
	for i := range scenes {
		if err := loadSceneDetails(ctx, pool, &scenes[i], playerToScene); err != nil {
			return persistedWorld{}, false, err
		}
	}

	return persistedWorld{
		PlayerToScene: playerToScene,
		Scenes:        scenes,
	}, true, nil
}

func persistWorldToStructuredDB(dsn, worldID string, worldData persistedWorld) error {
	worldID = normalizeWorldID(worldID)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect world db failed: %w", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "INSERT INTO worlds (id, name, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (id) DO UPDATE SET updated_at=NOW()", worldID, worldID); err != nil {
		return fmt.Errorf("upsert world failed: %w", err)
	}

	if err := deleteWorldData(ctx, tx, worldID); err != nil {
		return err
	}

	for _, snap := range worldData.Scenes {
		if err := persistScene(ctx, tx, worldID, snap); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx failed: %w", err)
	}

	// After commit, update embeddings (can be expensive).
	if err := upsertEmbeddingsForWorld(ctx, pool, worldData); err != nil {
		return err
	}
	return nil
}

func upsertEmbeddingsForWorld(ctx context.Context, pool *pgxpool.Pool, worldData persistedWorld) error {
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

	entityIDs := make([]string, 0, len(entries))
	for _, e := range entries {
		entityIDs = append(entityIDs, e.entityID)
	}
	rows, err := pool.Query(ctx, "SELECT entity_type, entity_id, text FROM embeddings WHERE entity_type = ANY($1) AND entity_id = ANY($2)", []string{"scene", "object"}, entityIDs)
	if err != nil {
		return fmt.Errorf("query embeddings failed: %w", err)
	}
	defer rows.Close()
	existing := map[string]string{}
	for rows.Next() {
		var et, eid, text string
		if err := rows.Scan(&et, &eid, &text); err != nil {
			return fmt.Errorf("scan embeddings failed: %w", err)
		}
		existing[et+":"+eid] = text
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate embeddings failed: %w", err)
	}

	toEmbed := make([]entry, 0)
	for _, e := range entries {
		key := e.entityType + ":" + e.entityID
		if existing[key] == e.text {
			continue
		}
		toEmbed = append(toEmbed, e)
	}
	if len(toEmbed) == 0 {
		return nil
	}

	batchSize := 50
	for i := 0; i < len(toEmbed); i += batchSize {
		end := i + batchSize
		if end > len(toEmbed) {
			end = len(toEmbed)
		}
		chunk := toEmbed[i:end]
		texts := make([]string, 0, len(chunk))
		for _, e := range chunk {
			texts = append(texts, e.text)
		}
		vectors, err := semantic.EmbedTexts(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed texts failed: %w", err)
		}
		if len(vectors) != len(chunk) {
			return fmt.Errorf("embedding size mismatch")
		}
		batch := &pgx.Batch{}
		for idx, e := range chunk {
			batch.Queue(
				"INSERT INTO embeddings (entity_type, entity_id, text, embedding, updated_at) VALUES ($1, $2, $3, $4, NOW()) ON CONFLICT (entity_type, entity_id) DO UPDATE SET text=EXCLUDED.text, embedding=EXCLUDED.embedding, updated_at=NOW()",
				e.entityType,
				e.entityID,
				e.text,
				pgvector.NewVector(float64ToFloat32(vectors[idx])),
			)
		}
		results := pool.SendBatch(ctx, batch)
		for range chunk {
			if _, err := results.Exec(); err != nil {
				results.Close()
				return fmt.Errorf("upsert embeddings failed: %w", err)
			}
		}
		results.Close()
	}
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

func deleteWorldData(ctx context.Context, tx pgx.Tx, worldID string) error {
	// Delete in FK-safe order by selecting scenes belonging to world.
	if _, err := tx.Exec(ctx, "DELETE FROM inventory USING players p JOIN scenes s ON p.scene_id=s.id WHERE inventory.player_id=p.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete inventory failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM player_layers USING players p JOIN scenes s ON p.scene_id=s.id WHERE player_layers.player_id=p.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete player_layers failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM players USING scenes s WHERE players.scene_id=s.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete players failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM object_tags USING objects o JOIN scenes s ON o.scene_id=s.id WHERE object_tags.object_id=o.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete object_tags failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM objects USING scenes s WHERE objects.scene_id=s.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete objects failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM layer_relations USING layers l JOIN scenes s ON l.scene_id=s.id WHERE (layer_relations.parent_layer_id=l.id OR layer_relations.child_layer_id=l.id) AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete layer_relations failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM layers USING scenes s WHERE layers.scene_id=s.id AND s.world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete layers failed: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM scenes WHERE world_id=$1", worldID); err != nil {
		return fmt.Errorf("delete scenes failed: %w", err)
	}
	return nil
}

func persistScene(ctx context.Context, tx pgx.Tx, worldID string, snap sceneSnapshot) error {
	sceneID := strings.TrimSpace(snap.ID)
	if sceneID == "" {
		return fmt.Errorf("scene id is required")
	}
	snap.Objects = mergeObjectsFromLayerItemStates(snap)

	meta := cloneMetaWithLayerStack(snap)
	metaJSON, _ := json.Marshal(meta)
	if _, err := tx.Exec(ctx, "INSERT INTO scenes (id, world_id, name, version, meta, updated_at) VALUES ($1, $2, $3, $4, $5, NOW())", sceneID, worldID, sceneID, snap.State.Version, metaJSON); err != nil {
		return fmt.Errorf("insert scene failed: %w", err)
	}

	layerIDs := collectLayerIDs(snap)
	layerDescs := asStringMap(snap.State.Meta, "layer_descriptions")
	layerBoot := asBoolMap(snap.State.Meta, "layer_bootstrapped")
	for _, layerID := range layerIDs {
		desc := strings.TrimSpace(layerDescs[layerID])
		boot := layerBoot[layerID]
		if _, err := tx.Exec(ctx, "INSERT INTO layers (id, scene_id, name, description, bootstrapped, updated_at) VALUES ($1, $2, $3, $4, $5, NOW())", layerID, sceneID, layerID, desc, boot); err != nil {
			return fmt.Errorf("insert layer %s failed: %w", layerID, err)
		}
	}

	layerParents := asStringMap(snap.State.Meta, "layer_parents")
	for child, parent := range layerParents {
		if strings.TrimSpace(child) == "" || strings.TrimSpace(parent) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, "INSERT INTO layer_relations (parent_layer_id, child_layer_id, relation) VALUES ($1, $2, $3)", parent, child, "child"); err != nil {
			return fmt.Errorf("insert layer relation failed: %w", err)
		}
	}

	for _, player := range snap.Players {
		if player == nil || strings.TrimSpace(player.ID) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, "INSERT INTO players (id, scene_id, name, updated_at) VALUES ($1, $2, $3, NOW())", player.ID, sceneID, player.Name); err != nil {
			return fmt.Errorf("insert player failed: %w", err)
		}
		for idx, layerID := range player.LayerStack {
			if strings.TrimSpace(layerID) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, "INSERT INTO player_layers (player_id, layer_id, pos) VALUES ($1, $2, $3)", player.ID, layerID, idx); err != nil {
				return fmt.Errorf("insert player layer failed: %w", err)
			}
		}
		for _, objID := range player.InventoryObjIDs {
			if strings.TrimSpace(objID) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, "INSERT INTO inventory (player_id, object_id) VALUES ($1, $2)", player.ID, objID); err != nil {
				return fmt.Errorf("insert inventory failed: %w", err)
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
		if _, err := tx.Exec(ctx, "INSERT INTO objects (id, scene_id, name, state, version, updated_at) VALUES ($1, $2, $3, $4, $5, NOW())", obj.ID, sceneID, obj.Name, stateJSON, obj.Version); err != nil {
			return fmt.Errorf("insert object failed: %w", err)
		}
		for _, tag := range obj.Tags {
			if strings.TrimSpace(tag) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, "INSERT INTO object_tags (object_id, tag) VALUES ($1, $2)", obj.ID, tag); err != nil {
				return fmt.Errorf("insert object tag failed: %w", err)
			}
		}
	}
	return nil
}

func loadSceneDetails(ctx context.Context, pool *pgxpool.Pool, snap *sceneSnapshot, playerToScene map[string]string) error {
	sceneID := strings.TrimSpace(snap.ID)
	if sceneID == "" {
		return nil
	}

	layers := map[string]struct{}{}
	layerRows, err := pool.Query(ctx, "SELECT id, description, bootstrapped FROM layers WHERE scene_id=$1", sceneID)
	if err != nil {
		return fmt.Errorf("query layers failed: %w", err)
	}
	for layerRows.Next() {
		var layerID string
		var desc string
		var boot bool
		if err := layerRows.Scan(&layerID, &desc, &boot); err != nil {
			layerRows.Close()
			return fmt.Errorf("scan layers failed: %w", err)
		}
		layers[layerID] = struct{}{}
		setMetaString(snap.State.Meta, "layer_descriptions", layerID, desc)
		setMetaBool(snap.State.Meta, "layer_bootstrapped", layerID, boot)
	}
	layerRows.Close()

	relRows, err := pool.Query(ctx, "SELECT parent_layer_id, child_layer_id, relation FROM layer_relations WHERE parent_layer_id IN (SELECT id FROM layers WHERE scene_id=$1) OR child_layer_id IN (SELECT id FROM layers WHERE scene_id=$1)", sceneID)
	if err != nil {
		return fmt.Errorf("query layer_relations failed: %w", err)
	}
	for relRows.Next() {
		var parent, child, rel string
		if err := relRows.Scan(&parent, &child, &rel); err != nil {
			relRows.Close()
			return fmt.Errorf("scan layer_relations failed: %w", err)
		}
		setMetaString(snap.State.Meta, "layer_parents", child, parent)
	}
	relRows.Close()

	objRows, err := pool.Query(ctx, "SELECT id, name, state, version FROM objects WHERE scene_id=$1", sceneID)
	if err != nil {
		return fmt.Errorf("query objects failed: %w", err)
	}
	objects := make([]*model.GameObject, 0)
	for objRows.Next() {
		var id, name string
		var stateRaw []byte
		var version int64
		if err := objRows.Scan(&id, &name, &stateRaw, &version); err != nil {
			objRows.Close()
			return fmt.Errorf("scan objects failed: %w", err)
		}
		state := map[string]any{}
		if len(stateRaw) > 0 {
			_ = json.Unmarshal(stateRaw, &state)
		}
		objects = append(objects, &model.GameObject{ID: id, Name: name, State: state, Version: version})
	}
	objRows.Close()

	tagRows, err := pool.Query(ctx, "SELECT object_id, tag FROM object_tags WHERE object_id IN (SELECT id FROM objects WHERE scene_id=$1)", sceneID)
	if err != nil {
		return fmt.Errorf("query object_tags failed: %w", err)
	}
	tags := map[string][]string{}
	for tagRows.Next() {
		var objID, tag string
		if err := tagRows.Scan(&objID, &tag); err != nil {
			tagRows.Close()
			return fmt.Errorf("scan object_tags failed: %w", err)
		}
		tags[objID] = append(tags[objID], tag)
	}
	tagRows.Close()
	for _, obj := range objects {
		obj.Tags = append(obj.Tags, tags[obj.ID]...)
	}
	for _, obj := range objects {
		if obj.State != nil {
			if layerID, ok := obj.State["layer"].(string); ok {
				layers[layerID] = struct{}{}
			}
		}
	}

	playerRows, err := pool.Query(ctx, "SELECT id, name FROM players WHERE scene_id=$1", sceneID)
	if err != nil {
		return fmt.Errorf("query players failed: %w", err)
	}
	players := make([]*model.PlayerState, 0)
	for playerRows.Next() {
		var id, name string
		if err := playerRows.Scan(&id, &name); err != nil {
			playerRows.Close()
			return fmt.Errorf("scan players failed: %w", err)
		}
		player := &model.PlayerState{ID: id, Name: name, SceneID: sceneID}
		players = append(players, player)
		playerToScene[id] = sceneID
	}
	playerRows.Close()

	layerRows2, err := pool.Query(ctx, "SELECT player_id, layer_id, pos FROM player_layers WHERE player_id IN (SELECT id FROM players WHERE scene_id=$1)", sceneID)
	if err != nil {
		return fmt.Errorf("query player_layers failed: %w", err)
	}
	layerByPlayer := map[string][]struct {
		pos   int
		layer string
	}{}
	for layerRows2.Next() {
		var pid, lid string
		var pos int
		if err := layerRows2.Scan(&pid, &lid, &pos); err != nil {
			layerRows2.Close()
			return fmt.Errorf("scan player_layers failed: %w", err)
		}
		layerByPlayer[pid] = append(layerByPlayer[pid], struct {
			pos   int
			layer string
		}{pos: pos, layer: lid})
	}
	layerRows2.Close()
	for _, player := range players {
		stack := layerByPlayer[player.ID]
		sort.Slice(stack, func(i, j int) bool { return stack[i].pos < stack[j].pos })
		player.LayerStack = nil
		for _, entry := range stack {
			player.LayerStack = append(player.LayerStack, entry.layer)
		}
	}

	invRows, err := pool.Query(ctx, "SELECT player_id, object_id FROM inventory WHERE player_id IN (SELECT id FROM players WHERE scene_id=$1)", sceneID)
	if err != nil {
		return fmt.Errorf("query inventory failed: %w", err)
	}
	invByPlayer := map[string][]string{}
	for invRows.Next() {
		var pid, oid string
		if err := invRows.Scan(&pid, &oid); err != nil {
			invRows.Close()
			return fmt.Errorf("scan inventory failed: %w", err)
		}
		invByPlayer[pid] = append(invByPlayer[pid], oid)
	}
	invRows.Close()
	for _, player := range players {
		player.InventoryObjIDs = invByPlayer[player.ID]
	}

	playerAttrs := asIntMapMap(snap.State.Meta, "player_attributes")
	playerDeviceIDs := asStringMap(snap.State.Meta, "player_device_id")
	playerDeviceLocks := asBoolMap(snap.State.Meta, "player_device_lock")
	for _, player := range players {
		attrs := playerAttrs[player.ID]
		player.Attributes = model.PlayerAttributePanel{
			Strength:     attrs["strength"],
			Dexterity:    attrs["dexterity"],
			Constitution: attrs["constitution"],
			Intelligence: attrs["intelligence"],
			Wisdom:       attrs["wisdom"],
			Charisma:     attrs["charisma"],
			Luck:         attrs["luck"],
		}
		player.Attributes.Normalize()
		if deviceID := strings.TrimSpace(playerDeviceIDs[player.ID]); deviceID != "" {
			player.DeviceID = deviceID
		}
		if locked, ok := playerDeviceLocks[player.ID]; ok {
			player.DeviceLocked = locked
		}
	}

	// Scene layer stack from meta if present.
	if raw, ok := snap.State.Meta["layer_stack"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					snap.LayerStack = append(snap.LayerStack, s)
				}
			}
		}
	}

	snap.Players = players
	snap.Objects = objects
	return nil
}

func cloneMetaWithLayerStack(snap sceneSnapshot) map[string]any {
	meta := map[string]any{}
	for k, v := range snap.State.Meta {
		meta[k] = v
	}
	if len(snap.LayerStack) > 0 {
		meta["layer_stack"] = append([]string{}, snap.LayerStack...)
	}
	playerAttrs := map[string]map[string]int{}
	playerDeviceIDs := map[string]string{}
	playerDeviceLocks := map[string]bool{}
	for _, player := range snap.Players {
		if player == nil || strings.TrimSpace(player.ID) == "" {
			continue
		}
		panel := player.Attributes
		panel.Normalize()
		playerAttrs[player.ID] = map[string]int{
			"strength":     panel.Strength,
			"dexterity":    panel.Dexterity,
			"constitution": panel.Constitution,
			"intelligence": panel.Intelligence,
			"wisdom":       panel.Wisdom,
			"charisma":     panel.Charisma,
			"luck":         panel.Luck,
		}
		if strings.TrimSpace(player.DeviceID) != "" {
			playerDeviceIDs[player.ID] = strings.TrimSpace(player.DeviceID)
		}
		if player.DeviceLocked {
			playerDeviceLocks[player.ID] = true
		}
	}
	if len(playerAttrs) > 0 {
		meta["player_attributes"] = playerAttrs
	}
	if len(playerDeviceIDs) > 0 {
		meta["player_device_id"] = playerDeviceIDs
	}
	if len(playerDeviceLocks) > 0 {
		meta["player_device_lock"] = playerDeviceLocks
	}
	return meta
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

func asIntMapMap(meta map[string]any, key string) map[string]map[string]int {
	out := map[string]map[string]int{}
	if meta == nil {
		return out
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return out
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range root {
		child, ok := v.(map[string]any)
		if !ok {
			continue
		}
		entry := map[string]int{}
		for ck, cv := range child {
			switch typed := cv.(type) {
			case int:
				entry[ck] = typed
			case int64:
				entry[ck] = int(typed)
			case float64:
				entry[ck] = int(typed)
			}
		}
		if len(entry) > 0 {
			out[k] = entry
		}
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

func setMetaString(meta map[string]any, key, id, value string) {
	if meta == nil {
		return
	}
	raw, ok := meta[key]
	var target map[string]any
	if ok {
		if v, ok := raw.(map[string]any); ok {
			target = v
		}
	}
	if target == nil {
		target = map[string]any{}
		meta[key] = target
	}
	target[id] = value
}

func setMetaBool(meta map[string]any, key, id string, value bool) {
	if meta == nil {
		return
	}
	raw, ok := meta[key]
	var target map[string]any
	if ok {
		if v, ok := raw.(map[string]any); ok {
			target = v
		}
	}
	if target == nil {
		target = map[string]any{}
		meta[key] = target
	}
	target[id] = value
}

func float64ToFloat32(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, v := range values {
		out[i] = float32(v)
	}
	return out
}
