package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"SAO/internal/ai"
	"SAO/internal/model"
	"SAO/internal/world"
)

type Handler struct {
	world   *world.Manager
	referee ai.Referee
}

var (
	errOutcomeTimeout = errors.New("wait for action outcome timeout")
	requestCounter    uint64
)

const actionOutcomeTimeout = 40 * time.Second

type NLActionRequest struct {
	PlayerID string `json:"player_id"`
	Input    string `json:"input"`
}

type LoginRequest struct {
	PlayerID   string         `json:"player_id"`
	Name       string         `json:"name,omitempty"`
	SceneID    string         `json:"scene_id,omitempty"`
	Attributes map[string]int `json:"attributes,omitempty"`
	DeviceID   string         `json:"device_id,omitempty"`
}

type LogoutRequest struct {
	PlayerID string `json:"player_id"`
	DeviceID string `json:"device_id"`
}

type AccountListRequest struct {
	SceneID string `json:"scene_id,omitempty"`
}

type CreateLayerRequest struct {
	PlayerID      string `json:"player_id"`
	LayerID       string `json:"layer_id"`
	Relation      string `json:"relation,omitempty"`
	ParentLayerID string `json:"parent_layer_id,omitempty"`
}

func NewHandler(worldManager *world.Manager, referee ai.Referee) *Handler {
	if referee == nil {
		referee = ai.NewRuleReferee()
	}
	return &Handler{world: worldManager, referee: referee}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/events", h.handleEvents)
	mux.HandleFunc("/auth/login", h.handleLogin)
	mux.HandleFunc("/auth/logout", h.handleLogout)
	mux.HandleFunc("/auth/accounts", h.handleAccountList)
	mux.HandleFunc("/action", h.handleAction)
	mux.HandleFunc("/nl-action", h.handleNaturalLanguageAction)
	mux.HandleFunc("/interact", h.handleInteractAction)
	mux.HandleFunc("/observe", h.handleObserveAction)
	mux.HandleFunc("/pick-drop", h.handlePickDropAction)
	mux.HandleFunc("/scene/layer", h.handleCreateLayer)
	mux.HandleFunc("/scene/nearby", h.handleNearbyScene)
	mux.HandleFunc("/player/panel/get", h.handleGetPlayerPanel)
	mux.HandleFunc("/player/panel/set", h.handleSetPlayerPanel)
	mux.HandleFunc("/player/inventory/get", h.handleGetPlayerInventory)
}

type InteractRequest struct {
	PlayerID       string         `json:"player_id"`
	Interaction    string         `json:"interaction"`
	TargetObjectID string         `json:"target_object_id,omitempty"`
	TargetQuery    string         `json:"target_query,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

type PickDropRequest struct {
	PlayerID       string         `json:"player_id"`
	Action         string         `json:"action"` // pick|drop
	TargetObjectID string         `json:"target_object_id,omitempty"`
	TargetQuery    string         `json:"target_query,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

type ObserveRequest struct {
	PlayerID       string         `json:"player_id"`
	TargetObjectID string         `json:"target_object_id,omitempty"`
	TargetQuery    string         `json:"target_query,omitempty"`
	LayerID        string         `json:"layer_id,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

type NearbySceneRequest struct {
	PlayerID string `json:"player_id"`
}

type PlayerPanelGetRequest struct {
	PlayerID string `json:"player_id"`
}

type PlayerPanelSetRequest struct {
	PlayerID   string         `json:"player_id"`
	Attributes map[string]int `json:"attributes"`
}

type PlayerInventoryGetRequest struct {
	PlayerID string `json:"player_id"`
}

func ensureRequestID(action *model.Action, playerID string) string {
	if action.Payload == nil {
		action.Payload = map[string]any{}
	}
	if existing, _ := action.Payload["request_id"].(string); strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing)
	}
	reqID := fmt.Sprintf("%s-%d-%d", strings.TrimSpace(playerID), time.Now().UnixNano(), atomic.AddUint64(&requestCounter, 1))
	action.Payload["request_id"] = reqID
	return reqID
}

func outcomeError(payload map[string]any) (string, bool) {
	if payload == nil {
		return "", false
	}
	v, _ := payload["error"].(string)
	v = strings.TrimSpace(v)
	return v, v != ""
}

func (h *Handler) enqueueAndWaitOutcome(envelope model.ActionEnvelope) (model.Event, error) {
	if strings.TrimSpace(envelope.PlayerID) == "" {
		return model.Event{}, fmt.Errorf("player_id is required")
	}
	reqID := ensureRequestID(&envelope.Action, envelope.PlayerID)

	subID, ch, err := h.world.SubscribePlayerEvents(envelope.PlayerID, 32)
	if err != nil {
		return model.Event{}, err
	}
	defer h.world.UnsubscribePlayerEvents(envelope.PlayerID, subID)

	if err := h.world.EnqueueAction(envelope); err != nil {
		return model.Event{}, err
	}

	timer := time.NewTimer(actionOutcomeTimeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return model.Event{}, fmt.Errorf("player event stream closed")
			}
			if ev.Type != "action_resolved" {
				continue
			}
			if reqID == "" {
				return ev, nil
			}
			eventReqID, _ := ev.Payload["request_id"].(string)
			if strings.TrimSpace(eventReqID) == reqID {
				return ev, nil
			}
		case <-timer.C:
			return model.Event{}, errOutcomeTimeout
		}
	}
}

func writeActionOutcome(w http.ResponseWriter, event model.Event, extra map[string]any) {
	resp := map[string]any{
		"outcome": event.Payload,
		"event":   event,
	}
	for k, v := range extra {
		resp[k] = v
	}
	if errMsg, ok := outcomeError(event.Payload); ok {
		resp["status"] = "failed"
		resp["error"] = errMsg
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}
	resp["status"] = "ok"
	writeJSON(w, http.StatusOK, resp)
}

func writeActionError(w http.ResponseWriter, err error, extra map[string]any) {
	resp := map[string]any{}
	for k, v := range extra {
		resp[k] = v
	}
	if errors.Is(err, errOutcomeTimeout) {
		resp["error"] = err.Error()
		writeJSON(w, http.StatusGatewayTimeout, resp)
		return
	}
	resp["error"] = err.Error()
	writeJSON(w, http.StatusBadRequest, resp)
}

func (h *Handler) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req model.ActionEnvelope
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}

	event, err := h.enqueueAndWaitOutcome(req)
	if err != nil {
		writeActionError(w, err, nil)
		return
	}
	writeActionOutcome(w, event, map[string]any{"action": req.Action})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id is required"})
		return
	}

	if sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID); err == nil {
		player, lockErr := sceneInstance.LockPlayerToDevice(req.PlayerID, deviceID)
		if lockErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": lockErr.Error()})
			return
		}
		if err := h.world.FlushPersistence(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"player":   player,
			"scene_id": player.SceneID,
		})
		return
	}

	sceneID := strings.TrimSpace(req.SceneID)
	if sceneID == "" {
		sceneID = "scene-城镇"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.PlayerID
	}
	player := &model.PlayerState{
		ID:           req.PlayerID,
		Name:         name,
		Attributes:   model.DefaultPlayerAttributePanel(),
		DeviceID:     deviceID,
		DeviceLocked: true,
	}
	if err := h.world.AddPlayer(sceneID, player); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(req.Attributes) > 0 {
		if _, err := sceneInstance.UpdatePlayerAttributePanel(req.PlayerID, req.Attributes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if err := h.world.FlushPersistence(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	created, err := sceneInstance.GetPlayerInfo(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"player":   created,
		"scene_id": sceneID,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id is required"})
		return
	}
	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	player, err := sceneInstance.UnlockPlayerDevice(req.PlayerID, req.DeviceID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := h.world.FlushPersistence(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"player": player,
	})
}

func (h *Handler) handleAccountList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req AccountListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	sceneIDFilter := strings.TrimSpace(req.SceneID)
	players := h.world.ListPlayers()
	resp := make([]map[string]any, 0, len(players))
	for _, p := range players {
		if p == nil {
			continue
		}
		if sceneIDFilter != "" && strings.TrimSpace(p.SceneID) != sceneIDFilter {
			continue
		}
		isAvailable := !p.DeviceLocked || strings.TrimSpace(p.DeviceID) == ""
		if !isAvailable {
			continue
		}
		resp = append(resp, map[string]any{
			"player_id":     p.ID,
			"name":          p.Name,
			"scene_id":      p.SceneID,
			"device_locked": p.DeviceLocked,
			"is_available":  isAvailable,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"accounts": resp,
	})
}

func (h *Handler) handleNaturalLanguageAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req NLActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	if req.Input == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "input is required"})
		return
	}

	parsed, err := h.referee.Call(req.Input)
	if err != nil {
		fallback, fallbackErr := ai.NewRuleReferee().Call(req.Input)
		if fallbackErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if fallback.Payload == nil {
			fallback.Payload = map[string]any{}
		}
		fallback.Payload["llm_unavailable"] = err.Error()
		parsed = fallback
	}
	action := ai.ToModelAction(parsed)
	envelope := model.ActionEnvelope{PlayerID: req.PlayerID, Action: action}
	event, err := h.enqueueAndWaitOutcome(envelope)
	if err != nil {
		writeActionError(w, err, map[string]any{"parsed_action": action})
		return
	}
	writeActionOutcome(w, event, map[string]any{"parsed_action": action})
}

func (h *Handler) handleInteractAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req InteractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	action := model.Action{
		Type:           "interact",
		Interaction:    req.Interaction,
		TargetObjectID: req.TargetObjectID,
		TargetQuery:    req.TargetQuery,
		Payload:        req.Payload,
	}
	event, err := h.enqueueAndWaitOutcome(model.ActionEnvelope{PlayerID: req.PlayerID, Action: action})
	if err != nil {
		writeActionError(w, err, map[string]any{"action": action})
		return
	}
	writeActionOutcome(w, event, map[string]any{"action": action})
}

func (h *Handler) handleObserveAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req ObserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	action := model.Action{
		Type:           "observe",
		TargetObjectID: req.TargetObjectID,
		TargetQuery:    req.TargetQuery,
		LayerID:        req.LayerID,
		Payload:        req.Payload,
	}
	event, err := h.enqueueAndWaitOutcome(model.ActionEnvelope{PlayerID: req.PlayerID, Action: action})
	if err != nil {
		writeActionError(w, err, map[string]any{"action": action})
		return
	}
	writeActionOutcome(w, event, map[string]any{"action": action})
}

func (h *Handler) handlePickDropAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req PickDropRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	interaction := ""
	switch req.Action {
	case "pick":
		interaction = "pickup_item"
	case "drop":
		interaction = "drop_item"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action must be pick or drop"})
		return
	}
	action := model.Action{
		Type:           "interact",
		Interaction:    interaction,
		TargetObjectID: req.TargetObjectID,
		TargetQuery:    req.TargetQuery,
		Payload:        req.Payload,
	}
	event, err := h.enqueueAndWaitOutcome(model.ActionEnvelope{PlayerID: req.PlayerID, Action: action})
	if err != nil {
		writeActionError(w, err, map[string]any{"action": action})
		return
	}
	writeActionOutcome(w, event, map[string]any{"action": action})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) handleCreateLayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req CreateLayerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	if req.LayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "layer_id is required"})
		return
	}
	createdID, err := h.world.CreateLayerForPlayer(req.PlayerID, req.LayerID, req.Relation, req.ParentLayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"layer_id": createdID,
		"outcome": map[string]any{
			"ok":                 true,
			"created_layer_id":   createdID,
			"relation":           req.Relation,
			"parent_layer_id":    req.ParentLayerID,
			"requested_layer_id": req.LayerID,
		},
	})
}

func (h *Handler) handleNearbyScene(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req NearbySceneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	payload, err := sceneInstance.GetNearbyLayersAndObjects(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func panelModifierMap(panel model.PlayerAttributePanel) map[string]int {
	return map[string]int{
		"strength":     model.ScoreModifier(panel.Strength),
		"dexterity":    model.ScoreModifier(panel.Dexterity),
		"constitution": model.ScoreModifier(panel.Constitution),
		"intelligence": model.ScoreModifier(panel.Intelligence),
		"wisdom":       model.ScoreModifier(panel.Wisdom),
		"charisma":     model.ScoreModifier(panel.Charisma),
		"luck":         model.ScoreModifier(panel.Luck),
	}
}

func (h *Handler) handleGetPlayerPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req PlayerPanelGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	panel, err := sceneInstance.GetPlayerAttributePanel(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"player_id":      req.PlayerID,
		"attributes":     panel,
		"attr_modifiers": panelModifierMap(panel),
	})
}

func (h *Handler) handleSetPlayerPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req PlayerPanelSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}
	if len(req.Attributes) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "attributes is required"})
		return
	}
	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	panel, err := sceneInstance.UpdatePlayerAttributePanel(req.PlayerID, req.Attributes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := h.world.FlushPersistence(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"player_id":      req.PlayerID,
		"attributes":     panel,
		"attr_modifiers": panelModifierMap(panel),
	})
}

func (h *Handler) handleGetPlayerInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()

	var req PlayerInventoryGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "player_id is required"})
		return
	}

	sceneInstance, err := h.world.GetSceneByPlayer(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := sceneInstance.GetPlayerInventory(req.PlayerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	resp := make([]map[string]any, 0, len(items))
	for _, obj := range items {
		if obj == nil {
			continue
		}
		layerID, _ := obj.State["layer"].(string)
		resp = append(resp, map[string]any{
			"id":          obj.ID,
			"name":        obj.Name,
			"layer":       layerID,
			"tags":        append([]string{}, obj.Tags...),
			"description": objectDescription(obj.State),
			"state":       obj.State,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"player_id": req.PlayerID,
		"items":     resp,
	})
}

func objectDescription(state map[string]any) string {
	if state == nil {
		return ""
	}
	if v, ok := state["description"].(string); ok {
		return v
	}
	if v, ok := state["desc"].(string); ok {
		return v
	}
	return ""
}
