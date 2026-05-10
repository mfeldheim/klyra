package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
)

// Handlers holds the dependencies for the HTTP API handlers.
type Handlers struct {
	store      *state.Store
	cfg        *config.Config
	incMgr     *incident.Manager    // nil if incidents not configured
	incStr     incident.Store       // nil if incidents not configured; used for list/get
	chatRunner incident.InvRunner   // nil if AI chat not configured
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(st *state.Store, cfg *config.Config) *Handlers {
	return &Handlers{store: st, cfg: cfg}
}

// SetIncidentManager wires incident support into the handlers.
func (h *Handlers) SetIncidentManager(mgr *incident.Manager, store incident.Store) {
	h.incMgr = mgr
	h.incStr = store
}

// SetChatRunner wires the AI chat runner into the handlers.
// This avoids a circular import between server and investigate packages.
func (h *Handlers) SetChatRunner(runner incident.InvRunner) {
	h.chatRunner = runner
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Status responds with all current alarm states and an updatedAt timestamp.
func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"alarms":    h.store.Alarms(),
		"updatedAt": time.Now(),
	})
}

// History responds with all recorded history events.
func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.History())
}

// Config responds with the current configuration.
func (h *Handlers) Config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.cfg)
}

// Silences responds with all current silences.
func (h *Handlers) Silences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Silences())
}

// silenceRequest is the expected body for CreateSilence.
type silenceRequest struct {
	Monitor  string `json:"monitor"`
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

// CreateSilence parses a JSON body, creates a Silence, and stores it.
func (h *Handlers) CreateSilence(w http.ResponseWriter, r *http.Request) {
	var req silenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	dur, err := time.ParseDuration(req.Duration)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}

	sl := state.Silence{
		ID:          uuid.NewString(),
		MonitorName: req.Monitor,
		Until:       time.Now().Add(dur),
		Reason:      req.Reason,
	}
	h.store.AddSilence(sl)
	writeJSON(w, http.StatusCreated, sl)
}

// DeleteSilence removes a silence by ID extracted from the URL path suffix.
func (h *Handlers) DeleteSilence(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/silences/")
	if id == "" {
		http.Error(w, "missing silence id", http.StatusBadRequest)
		return
	}
	if !h.store.RemoveSilence(id) {
		http.Error(w, "silence not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Me responds with the authenticated user extracted from oauth2-proxy headers.
func Me(w http.ResponseWriter, r *http.Request) {
	user := ""
	for _, hdr := range []string{
		"X-Auth-Request-Preferred-Username",
		"X-Auth-Request-User",
		"X-Forwarded-User",
	} {
		if v := r.Header.Get(hdr); v != "" {
			user = v
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"user": user})
}

// --- Incident handlers ---

func (h *Handlers) incidentsEnabled(w http.ResponseWriter) bool {
	if h.incStr == nil {
		http.Error(w, "incidents not configured", http.StatusNotImplemented)
		return false
	}
	return true
}

// ListIncidents returns the incident index from S3.
func (h *Handlers) ListIncidents(w http.ResponseWriter, r *http.Request) {
	if !h.incidentsEnabled(w) {
		return
	}
	idx, err := h.incStr.ListIncidents(r.Context())
	if err != nil {
		http.Error(w, "failed to list incidents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, idx)
}

// GetIncident returns incident metadata by ID from S3.
func (h *Handlers) GetIncident(w http.ResponseWriter, r *http.Request) {
	if !h.incidentsEnabled(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/incidents/")
	id = strings.TrimSuffix(id, "/stream")
	id = strings.TrimSuffix(id, "/chat")
	if id == "" {
		http.Error(w, "missing incident id", http.StatusBadRequest)
		return
	}
	inc, err := h.incStr.ReadIncident(r.Context(), id)
	if err != nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	active := h.incMgr != nil && h.incMgr.IsActive(inc.ID)
	writeJSON(w, http.StatusOK, map[string]any{"incident": inc, "active": active})
}

// writeSSE writes a single SSE event to the response.
func writeSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// StreamIncident streams the investigation output via SSE.
// If the investigation is live, it streams deltas in real time.
// If complete, it sends the full buffered content in one shot.
func (h *Handlers) StreamIncident(w http.ResponseWriter, r *http.Request) {
	if !h.incidentsEnabled(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/incidents/")
	id = strings.TrimSuffix(id, "/stream")
	if id == "" {
		http.Error(w, "missing incident id", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Try live stream first
	if h.incMgr != nil {
		if ch := h.incMgr.Subscribe(id); ch != nil {
			defer h.incMgr.Unsubscribe(id, ch)
			for {
				select {
				case text, ok := <-ch:
					if !ok {
						data, _ := json.Marshal(map[string]string{})
						writeSSE(w, "done", string(data))
						return
					}
					data, _ := json.Marshal(map[string]string{"text": text})
					writeSSE(w, "delta", string(data))
				case <-r.Context().Done():
					return
				}
			}
		}
	}

	// Incident not active — replay full markdown content from S3
	content, err := h.incStr.ReadContent(r.Context(), id)
	if err != nil {
		// Fallback: incident exists but content unreadable
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	data, _ := json.Marshal(map[string]string{"text": content})
	writeSSE(w, "delta", string(data))
	doneData, _ := json.Marshal(map[string]string{})
	writeSSE(w, "done", string(doneData))
}

type chatRequest struct {
	Message string `json:"message"`
}

// ChatIncident appends a user message to the investigation and streams the response.
func (h *Handlers) ChatIncident(w http.ResponseWriter, r *http.Request) {
	if !h.incidentsEnabled(w) {
		return
	}
	if h.incMgr == nil {
		http.Error(w, "incidents not configured", http.StatusNotImplemented)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/incidents/")
	id = strings.TrimSuffix(id, "/chat")
	if id == "" {
		http.Error(w, "missing incident id", http.StatusBadRequest)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !h.incMgr.IsActive(id) {
		http.Error(w, "incident not active — chat session ended", http.StatusGone)
		return
	}

	if h.chatRunner == nil {
		http.Error(w, "chat not configured", http.StatusNotImplemented)
		return
	}

	ch, err := h.incMgr.Chat(r.Context(), id, req.Message, h.chatRunner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	for {
		select {
		case text, ok := <-ch:
			if !ok {
				doneData, _ := json.Marshal(map[string]string{})
				writeSSE(w, "done", string(doneData))
				return
			}
			data, _ := json.Marshal(map[string]string{"text": text})
			writeSSE(w, "delta", string(data))
		case <-r.Context().Done():
			return
		}
	}
}

// ensure context import is used
var _ = context.Background
