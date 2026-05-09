package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/state"
)

// Handlers holds the dependencies for the HTTP API handlers.
type Handlers struct {
	store *state.Store
	cfg   *config.Config
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(st *state.Store, cfg *config.Config) *Handlers {
	return &Handlers{store: st, cfg: cfg}
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
