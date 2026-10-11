package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Handlers holds the dependencies for the HTTP API handlers.
type Handlers struct {
	store     *state.Store
	cfg       *config.Config
	incMgr    *incident.Manager    // nil if incidents not configured
	incStr    incident.Store       // nil if incidents not configured; used for list/get
	k8sClient kubernetes.Interface // optional; required for workload log streaming
	mcp       http.Handler         // optional; MCP server mounted at /mcp
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

// SetMCPHandler wires the MCP server handler into the handlers.
func (h *Handlers) SetMCPHandler(handler http.Handler) {
	h.mcp = handler
}

// SetK8sClient wires a Kubernetes client into handlers that need cluster reads.
func (h *Handlers) SetK8sClient(client kubernetes.Interface) {
	h.k8sClient = client
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

// Me responds with the authenticated user. Identity is injected by Cloudflare
// Access as the Cf-Access-Authenticated-User-Email header.
func Me(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("Cf-Access-Authenticated-User-Email")
	if user == "" {
		// Fallback for local development / non-Access deployments.
		user = r.Header.Get("X-Forwarded-User")
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

// GetIncident returns incident metadata and its recorded log by ID from S3.
func (h *Handlers) GetIncident(w http.ResponseWriter, r *http.Request) {
	if !h.incidentsEnabled(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/incidents/")
	if id == "" {
		http.Error(w, "missing incident id", http.StatusBadRequest)
		return
	}
	inc, err := h.incStr.ReadIncident(r.Context(), id)
	if err != nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	content, _ := h.incStr.ReadContent(r.Context(), id)
	active := h.incMgr != nil && h.incMgr.IsActive(inc.ID)
	writeJSON(w, http.StatusOK, map[string]any{"incident": inc, "active": active, "content": content})
}

// writeSSE writes a single SSE event to the response.
func writeSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// WorkloadLogs streams logs (SSE) from one pod of the target workload.
// Query params: kind=deploy|sts|ds, namespace, name, follow=true|false
func (h *Handlers) WorkloadLogs(w http.ResponseWriter, r *http.Request) {
	if h.k8sClient == nil {
		http.Error(w, "kubernetes client not configured", http.StatusNotImplemented)
		return
	}

	kind := r.URL.Query().Get("kind")
	namespace := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")
	follow := r.URL.Query().Get("follow") != "false"
	if kind == "" || namespace == "" || name == "" {
		http.Error(w, "kind, namespace and name are required", http.StatusBadRequest)
		return
	}

	var selector string
	var err error
	switch kind {
	case "deploy":
		dep, getErr := h.k8sClient.AppsV1().Deployments(namespace).Get(r.Context(), name, metav1.GetOptions{})
		if getErr != nil {
			err = getErr
			break
		}
		selector = metav1.FormatLabelSelector(dep.Spec.Selector)
	case "sts":
		sts, getErr := h.k8sClient.AppsV1().StatefulSets(namespace).Get(r.Context(), name, metav1.GetOptions{})
		if getErr != nil {
			err = getErr
			break
		}
		selector = metav1.FormatLabelSelector(sts.Spec.Selector)
	case "ds":
		ds, getErr := h.k8sClient.AppsV1().DaemonSets(namespace).Get(r.Context(), name, metav1.GetOptions{})
		if getErr != nil {
			err = getErr
			break
		}
		selector = metav1.FormatLabelSelector(ds.Spec.Selector)
	default:
		http.Error(w, "kind must be one of deploy|sts|ds", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "failed to load workload: "+err.Error(), http.StatusNotFound)
		return
	}
	if selector == "" {
		http.Error(w, "workload has empty selector", http.StatusBadRequest)
		return
	}

	pods, err := h.k8sClient.CoreV1().Pods(namespace).List(r.Context(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		http.Error(w, "failed to list pods: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(pods.Items) == 0 {
		http.Error(w, "no pods found for workload", http.StatusNotFound)
		return
	}

	pod := pickPodForLogs(pods.Items)
	tail := int64(200)
	stream, err := h.k8sClient.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow:     follow,
		Timestamps: true,
		TailLines:  &tail,
	}).Stream(r.Context())
	if err != nil {
		http.Error(w, "failed to stream logs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	meta, _ := json.Marshal(map[string]string{"pod": pod.Name, "namespace": namespace})
	writeSSE(w, "meta", string(meta))

	scanner := bufio.NewScanner(stream)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		payload, _ := json.Marshal(map[string]string{"line": fmt.Sprintf("[%s] %s", pod.Name, line)})
		writeSSE(w, "line", string(payload))
	}
	if scanErr := scanner.Err(); scanErr != nil && r.Context().Err() == nil {
		errData, _ := json.Marshal(map[string]string{"error": scanErr.Error()})
		writeSSE(w, "error", string(errData))
	}
	doneData, _ := json.Marshal(map[string]string{})
	writeSSE(w, "done", string(doneData))
}

func pickPodForLogs(pods []corev1.Pod) corev1.Pod {
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning {
			return pod
		}
	}
	return pods[0]
}
