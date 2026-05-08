# Klyra Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Klyra, a Kubernetes-native monitoring tool with config-driven monitors, pluggable modules, and a React status dashboard.

**Architecture:** Per-monitor goroutines feed a shared results channel; an evaluator applies threshold + for-duration logic and writes alarm state to a Kubernetes ConfigMap every 10s; leader election ensures only one replica runs the engine while all replicas serve the HTTP/React UI.

**Tech Stack:** Go 1.24 (alpine), `k8s.io/client-go`, `gopkg.in/yaml.v3`, `github.com/spf13/cobra`, `github.com/prometheus/client_golang`, React 18 + Vite + TypeScript, Helm 3, GitHub Actions

---

## Section 1 — Project Scaffold, Core Types, Config

---

### Task 1: Initialise Go module and project skeleton

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `.gitignore`

- [ ] **Step 1: Create go.mod**

```
module github.com/mfeldheim/klyra

go 1.24
```

- [ ] **Step 2: Create main.go stub**

```go
package main

import "github.com/mfeldheim/klyra/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 3: Create .gitignore**

```
klyra
dist/
ui/node_modules/
ui/dist/
.env
*.test
```

- [ ] **Step 4: Create directory structure**

```bash
mkdir -p internal/{config,engine,monitor/{kubernetes,http,prometheus},action/http,state,server,leader}
mkdir -p cmd ui deploy/helm/klyra/templates deploy/raw .github/workflows
```

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "chore: initialise Go module and project skeleton"
```

---

### Task 2: Core state types

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/state/state_test.go
package state_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func TestSilenceIsActive(t *testing.T) {
	s := state.Silence{Until: time.Now().Add(time.Hour)}
	if !s.IsActive(time.Now()) {
		t.Fatal("expected silence to be active")
	}
	expired := state.Silence{Until: time.Now().Add(-time.Minute)}
	if expired.IsActive(time.Now()) {
		t.Fatal("expected expired silence to be inactive")
	}
}

func TestPersistedStateTrim(t *testing.T) {
	ps := &state.PersistedState{}
	old := time.Now().Add(-25 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	ps.History = []state.HistoryEvent{
		{At: old, MonitorName: "a"},
		{At: recent, MonitorName: "b"},
	}
	ps.Trim(24 * time.Hour)
	if len(ps.History) != 1 || ps.History[0].MonitorName != "b" {
		t.Fatalf("expected 1 recent event, got %+v", ps.History)
	}
}
```

- [ ] **Step 2: Run test — expect compile failure**

```bash
cd /path/to/klyra && go test ./internal/state/...
```

Expected: `cannot find package`

- [ ] **Step 3: Implement state.go**

```go
// internal/state/state.go
package state

import "time"

type CheckStatus string

const (
	CheckOK      CheckStatus = "OK"
	CheckError   CheckStatus = "ERROR"
	CheckUnknown CheckStatus = "UNKNOWN"
)

type AlarmStatus string

const (
	AlarmOK      AlarmStatus = "OK"
	AlarmFiring  AlarmStatus = "FIRING"
	AlarmUnknown AlarmStatus = "UNKNOWN"
)

type Transition string

const (
	TransitionFiring   Transition = "FIRING"
	TransitionResolved Transition = "RESOLVED"
)

type CheckResult struct {
	MonitorName string
	Status      CheckStatus
	Value       any
	Message     string
	Timestamp   time.Time
}

type AlarmState struct {
	MonitorName  string      `json:"monitorName"`
	Status       AlarmStatus `json:"status"`
	LastCheck    time.Time   `json:"lastCheck"`
	FiredAt      *time.Time  `json:"firedAt,omitempty"`
	PendingSince *time.Time  `json:"pendingSince,omitempty"`
	LastValue    any         `json:"lastValue,omitempty"`
	Message      string      `json:"message,omitempty"`
}

type HistoryEvent struct {
	MonitorName string     `json:"monitorName"`
	Transition  Transition `json:"transition"`
	At          time.Time  `json:"at"`
	Message     string     `json:"message,omitempty"`
}

type AlarmEvent struct {
	MonitorName string
	Transition  Transition
	Message     string
	Value       any
	FiredAt     time.Time
}

type Silence struct {
	ID          string    `json:"id"`
	MonitorName string    `json:"monitorName"`
	Until       time.Time `json:"until"`
	Reason      string    `json:"reason,omitempty"`
}

func (s Silence) IsActive(now time.Time) bool {
	return now.Before(s.Until)
}

type PersistedState struct {
	UpdatedAt time.Time             `json:"updatedAt"`
	Alarms    map[string]AlarmState `json:"alarms"`
	History   []HistoryEvent        `json:"history"`
	Silences  []Silence             `json:"silences"`
}

func (ps *PersistedState) Trim(window time.Duration) {
	cutoff := time.Now().Add(-window)
	filtered := ps.History[:0]
	for _, e := range ps.History {
		if e.At.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	ps.History = filtered
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/state/... -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat: add core state types"
```

---

### Task 3: Config types

**Files:**
- Create: `internal/config/types.go`

- [ ] **Step 1: Create config types**

```go
// internal/config/types.go
package config

import "time"

type Config struct {
	Monitors []MonitorConfig `yaml:"monitors"`
	Actions  []ActionConfig  `yaml:"actions"`
}

type MonitorConfig struct {
	Name      string            `yaml:"name"`
	Type      string            `yaml:"type"`
	Interval  Duration          `yaml:"interval"`
	Config    map[string]any    `yaml:"config"`
	Threshold ThresholdConfig   `yaml:"threshold"`
	Actions   []string          `yaml:"actions"`
}

type ThresholdConfig struct {
	Operator string   `yaml:"operator"`
	Value    any      `yaml:"value"`
	For      Duration `yaml:"for"`
}

type ActionConfig struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

// Duration wraps time.Duration for YAML unmarshalling (e.g. "30s", "2m").
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/config/types.go
git commit -m "feat: add config types"
```

---

### Task 4: Config loader with env-var interpolation

**Files:**
- Create: `internal/config/loader.go`
- Create: `internal/config/loader_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/config/loader_test.go
package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mfeldheim/klyra/internal/config"
)

const testYAML = `
monitors:
  - name: test-http
    type: http
    interval: 30s
    config:
      url: https://example.com
      expect_status: 200
    threshold:
      operator: eq
      value: false
      for: 1m
    actions:
      - notify
actions:
  - name: notify
    type: http
    config:
      url: https://ntfy.sh/test
      auth:
        type: bearer
        token: ${TEST_TOKEN}
`

func TestLoadConfig(t *testing.T) {
	os.Setenv("TEST_TOKEN", "secret123")
	defer os.Unsetenv("TEST_TOKEN")

	cfg, err := config.Load(strings.NewReader(testYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(cfg.Monitors))
	}
	if cfg.Monitors[0].Name != "test-http" {
		t.Errorf("unexpected monitor name: %s", cfg.Monitors[0].Name)
	}
	if cfg.Monitors[0].Interval.Seconds() != 30 {
		t.Errorf("unexpected interval: %v", cfg.Monitors[0].Interval)
	}
	// env var interpolation
	actionCfg := cfg.Actions[0].Config
	authMap, _ := actionCfg["auth"].(map[string]any)
	if authMap["token"] != "secret123" {
		t.Errorf("expected token secret123, got %v", authMap["token"])
	}
}

func TestLoadConfigMissingEnvVar(t *testing.T) {
	os.Unsetenv("TEST_TOKEN")
	_, err := config.Load(strings.NewReader(testYAML))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/config/... 2>&1 | head -5
```

- [ ] **Step 3: Implement loader.go**

```go
// internal/config/loader.go
package config

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var envVarRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func Load(r io.Reader) (*Config, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	interpolated, err := interpolateEnv(string(raw))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal([]byte(interpolated), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func interpolateEnv(s string) (string, error) {
	var missing []string
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		key := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(key)
		if !ok {
			missing = append(missing, key)
			return match
		}
		return val
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("missing env vars: %s", strings.Join(missing, ", "))
	}
	return result, nil
}
```

- [ ] **Step 4: Add gopkg.in/yaml.v3 dependency**

```bash
go get gopkg.in/yaml.v3
go mod tidy
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./internal/config/... -v
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: add config loader with env-var interpolation"
```

---

### Task 5: Monitor interface and goroutine runner

**Files:**
- Create: `internal/monitor/monitor.go`
- Create: `internal/monitor/runner.go`
- Create: `internal/monitor/runner_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/monitor/runner_test.go
package monitor_test

import (
	"context"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

type fakeMonitor struct {
	name   string
	result state.CheckResult
}

func (f *fakeMonitor) Name() string { return f.name }
func (f *fakeMonitor) Check(_ context.Context) (state.CheckResult, error) {
	return f.result, nil
}

func TestRunnerSendsResults(t *testing.T) {
	results := make(chan state.CheckResult, 5)
	m := &fakeMonitor{
		name:   "test",
		result: state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go monitor.Run(ctx, m, 50*time.Millisecond, results)

	var got []state.CheckResult
	for r := range results {
		got = append(got, r)
		if len(got) >= 2 {
			break
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(got))
	}
	if got[0].MonitorName != "test" {
		t.Errorf("unexpected monitor name: %s", got[0].MonitorName)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/monitor/... 2>&1 | head -5
```

- [ ] **Step 3: Implement monitor.go**

```go
// internal/monitor/monitor.go
package monitor

import (
	"context"

	"github.com/mfeldheim/klyra/internal/state"
)

type Monitor interface {
	Name() string
	Check(ctx context.Context) (state.CheckResult, error)
}

type Factory func(name string, cfg map[string]any) (Monitor, error)
```

- [ ] **Step 4: Implement runner.go**

```go
// internal/monitor/runner.go
package monitor

import (
	"context"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func Run(ctx context.Context, m Monitor, interval time.Duration, results chan<- state.CheckResult) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r, err := m.Check(ctx)
			if err != nil {
				r = state.CheckResult{
					MonitorName: m.Name(),
					Status:      state.CheckUnknown,
					Message:     err.Error(),
					Timestamp:   time.Now(),
				}
			}
			if r.MonitorName == "" {
				r.MonitorName = m.Name()
			}
			if r.Timestamp.IsZero() {
				r.Timestamp = time.Now()
			}
			results <- r
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./internal/monitor/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/monitor/monitor.go internal/monitor/runner.go internal/monitor/runner_test.go
git commit -m "feat: add monitor interface and goroutine runner"
```

---

### Task 6: Action interface

**Files:**
- Create: `internal/action/action.go`

- [ ] **Step 1: Implement action.go**

```go
// internal/action/action.go
package action

import (
	"context"

	"github.com/mfeldheim/klyra/internal/state"
)

type Action interface {
	Name() string
	Fire(ctx context.Context, event state.AlarmEvent) error
}

type Factory func(name string, cfg map[string]any) (Action, error)
```

- [ ] **Step 2: Commit**

```bash
git add internal/action/action.go
git commit -m "feat: add action interface"
```

---

### Task 7: Monitor and action registries

**Files:**
- Create: `internal/monitor/registry.go`
- Create: `internal/action/registry.go`

- [ ] **Step 1: Implement monitor registry**

```go
// internal/monitor/registry.go
package monitor

import "fmt"

var factories = map[string]Factory{}

func Register(typ string, f Factory) {
	factories[typ] = f
}

func New(typ, name string, cfg map[string]any) (Monitor, error) {
	f, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown monitor type %q", typ)
	}
	return f(name, cfg)
}
```

- [ ] **Step 2: Implement action registry**

```go
// internal/action/registry.go
package action

import "fmt"

var factories = map[string]Factory{}

func Register(typ string, f Factory) {
	factories[typ] = f
}

func New(typ, name string, cfg map[string]any) (Action, error) {
	f, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown action type %q", typ)
	}
	return f(name, cfg)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/monitor/registry.go internal/action/registry.go
git commit -m "feat: add monitor and action registries"
```

---

*Section 1 complete. Continuing in Section 2 — State store + Engine.*

---

## Section 2 — State Store + Engine (Evaluator, Dispatcher, State Writer, Orchestrator)

---

### Task 8: In-memory state store with ConfigMap persistence

**Files:**
- Create: `internal/state/store.go`
- Create: `internal/state/store_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/state/store_test.go
package state_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func TestStoreGetSet(t *testing.T) {
	s := state.NewStore()
	alarm := state.AlarmState{MonitorName: "test", Status: state.AlarmOK}
	s.SetAlarm(alarm)

	got, ok := s.GetAlarm("test")
	if !ok {
		t.Fatal("expected alarm to exist")
	}
	if got.MonitorName != "test" {
		t.Errorf("unexpected name: %s", got.MonitorName)
	}
}

func TestStoreAppendHistory(t *testing.T) {
	s := state.NewStore()
	s.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionFiring, At: time.Now()})
	s.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionResolved, At: time.Now()})

	h := s.History()
	if len(h) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(h))
	}
}

func TestStoreDirty(t *testing.T) {
	s := state.NewStore()
	if s.IsDirty() {
		t.Fatal("new store should not be dirty")
	}
	s.SetAlarm(state.AlarmState{MonitorName: "x"})
	if !s.IsDirty() {
		t.Fatal("store should be dirty after SetAlarm")
	}
	s.ClearDirty()
	if s.IsDirty() {
		t.Fatal("store should not be dirty after ClearDirty")
	}
}

func TestStoreActiveSilences(t *testing.T) {
	s := state.NewStore()
	s.AddSilence(state.Silence{ID: "1", MonitorName: "test", Until: time.Now().Add(time.Hour)})
	s.AddSilence(state.Silence{ID: "2", MonitorName: "other", Until: time.Now().Add(-time.Minute)})

	if !s.IsSilenced("test") {
		t.Error("expected test to be silenced")
	}
	if s.IsSilenced("other") {
		t.Error("expected other not to be silenced (expired)")
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/state/... 2>&1 | head -5
```

- [ ] **Step 3: Implement store.go**

```go
// internal/state/store.go
package state

import (
	"encoding/json"
	"sync"
	"time"
)

type Store struct {
	mu       sync.RWMutex
	alarms   map[string]AlarmState
	history  []HistoryEvent
	silences []Silence
	dirty    bool
}

func NewStore() *Store {
	return &Store{alarms: make(map[string]AlarmState)}
}

func (s *Store) GetAlarm(name string) (AlarmState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.alarms[name]
	return a, ok
}

func (s *Store) SetAlarm(a AlarmState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alarms[a.MonitorName] = a
	s.dirty = true
}

func (s *Store) Alarms() map[string]AlarmState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]AlarmState, len(s.alarms))
	for k, v := range s.alarms {
		out[k] = v
	}
	return out
}

func (s *Store) AppendHistory(e HistoryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, e)
	s.dirty = true
}

func (s *Store) History() []HistoryEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]HistoryEvent, len(s.history))
	copy(out, s.history)
	return out
}

func (s *Store) AddSilence(sl Silence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.silences = append(s.silences, sl)
	s.dirty = true
}

func (s *Store) RemoveSilence(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sl := range s.silences {
		if sl.ID == id {
			s.silences = append(s.silences[:i], s.silences[i+1:]...)
			s.dirty = true
			return true
		}
	}
	return false
}

func (s *Store) Silences() []Silence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Silence, len(s.silences))
	copy(out, s.silences)
	return out
}

func (s *Store) IsSilenced(monitorName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	for _, sl := range s.silences {
		if sl.IsActive(now) && (sl.MonitorName == "" || sl.MonitorName == monitorName) {
			return true
		}
	}
	return false
}

func (s *Store) IsDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}

func (s *Store) ClearDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = false
}

// Snapshot returns a PersistedState trimmed to the given window.
func (s *Store) Snapshot(window time.Duration) PersistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ps := PersistedState{
		UpdatedAt: time.Now(),
		Alarms:    make(map[string]AlarmState, len(s.alarms)),
		History:   make([]HistoryEvent, len(s.history)),
		Silences:  make([]Silence, len(s.silences)),
	}
	for k, v := range s.alarms {
		ps.Alarms[k] = v
	}
	copy(ps.History, s.history)
	copy(ps.Silences, s.silences)
	ps.Trim(window)
	return ps
}

// LoadSnapshot replaces store state from a persisted snapshot.
func (s *Store) LoadSnapshot(ps PersistedState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ps.Alarms != nil {
		s.alarms = ps.Alarms
	}
	s.history = ps.History
	s.silences = ps.Silences
	s.dirty = false
}

func (s *Store) MarshalJSON() ([]byte, error) {
	ps := s.Snapshot(24 * time.Hour)
	return json.Marshal(ps)
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/state/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/state/store.go internal/state/store_test.go
git commit -m "feat: add in-memory state store"
```

---

### Task 9: Evaluator

**Files:**
- Create: `internal/engine/evaluator.go`
- Create: `internal/engine/evaluator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/engine/evaluator_test.go
package engine_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestEvaluateThresholdLt(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "lt", Value: float64(2)}
	if !engine.EvaluateThreshold(thr, float64(1)) {
		t.Error("1 < 2 should be true")
	}
	if engine.EvaluateThreshold(thr, float64(3)) {
		t.Error("3 < 2 should be false")
	}
}

func TestEvaluateThresholdEqBool(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "eq", Value: false}
	if !engine.EvaluateThreshold(thr, false) {
		t.Error("false eq false should be true")
	}
	if engine.EvaluateThreshold(thr, true) {
		t.Error("true eq false should be false")
	}
}

func TestEvaluateThresholdContains(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "contains", Value: "error"}
	if !engine.EvaluateThreshold(thr, "some error occurred") {
		t.Error("expected contains to match")
	}
}

func TestApplyResultFiresAfterForDuration(t *testing.T) {
	st := state.NewStore()
	thr := config.ThresholdConfig{
		Operator: "lt",
		Value:    float64(2),
		For:      config.Duration{Duration: 100 * time.Millisecond},
	}
	result := state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: float64(1), Timestamp: time.Now()}

	ev := engine.ApplyResult(st, thr, result)
	if ev != nil {
		t.Fatal("should not fire before for-duration elapses")
	}

	time.Sleep(110 * time.Millisecond)
	result.Timestamp = time.Now()
	ev = engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionFiring {
		t.Fatalf("expected FIRING event, got %v", ev)
	}
}

func TestApplyResultResolvesWhenThresholdClears(t *testing.T) {
	st := state.NewStore()
	// Pre-set alarm as FIRING
	now := time.Now()
	st.SetAlarm(state.AlarmState{
		MonitorName: "test",
		Status:      state.AlarmFiring,
		FiredAt:     &now,
	})
	thr := config.ThresholdConfig{Operator: "lt", Value: float64(2)}
	result := state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: float64(5), Timestamp: time.Now()}

	ev := engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionResolved {
		t.Fatalf("expected RESOLVED event, got %v", ev)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/engine/... 2>&1 | head -5
```

- [ ] **Step 3: Implement evaluator.go**

```go
// internal/engine/evaluator.go
package engine

import (
	"fmt"
	"regexp"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/state"
)

// EvaluateThreshold returns true if the threshold condition is met.
func EvaluateThreshold(thr config.ThresholdConfig, value any) bool {
	switch thr.Operator {
	case "lt":
		return toFloat(value) < toFloat(thr.Value)
	case "gt":
		return toFloat(value) > toFloat(thr.Value)
	case "lte":
		return toFloat(value) <= toFloat(thr.Value)
	case "gte":
		return toFloat(value) >= toFloat(thr.Value)
	case "eq":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", thr.Value)
	case "neq":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", thr.Value)
	case "contains":
		return containsStr(value, thr.Value)
	case "matches":
		re, err := regexp.Compile(fmt.Sprintf("%v", thr.Value))
		if err != nil {
			return false
		}
		return re.MatchString(fmt.Sprintf("%v", value))
	}
	return false
}

// ApplyResult evaluates a CheckResult against a threshold, updates store state,
// and returns an AlarmEvent if a state transition occurred (or nil).
func ApplyResult(st *state.Store, thr config.ThresholdConfig, r state.CheckResult) *state.AlarmEvent {
	current, _ := st.GetAlarm(r.MonitorName)
	met := EvaluateThreshold(thr, r.Value)
	now := r.Timestamp

	updated := state.AlarmState{
		MonitorName: r.MonitorName,
		Status:      current.Status,
		LastCheck:   now,
		FiredAt:     current.FiredAt,
		PendingSince: current.PendingSince,
		LastValue:   r.Value,
		Message:     r.Message,
	}

	if r.Status == state.CheckUnknown {
		updated.Status = state.AlarmUnknown
		updated.PendingSince = nil
		st.SetAlarm(updated)
		return nil
	}

	var event *state.AlarmEvent

	if met {
		if updated.PendingSince == nil {
			updated.PendingSince = &now
		}
		forDur := thr.For.Duration
		if forDur == 0 || now.Sub(*updated.PendingSince) >= forDur {
			if current.Status != state.AlarmFiring {
				updated.Status = state.AlarmFiring
				updated.FiredAt = &now
				event = &state.AlarmEvent{
					MonitorName: r.MonitorName,
					Transition:  state.TransitionFiring,
					Message:     r.Message,
					Value:       r.Value,
					FiredAt:     now,
				}
			} else {
				updated.Status = state.AlarmFiring
			}
		}
	} else {
		updated.PendingSince = nil
		if current.Status == state.AlarmFiring {
			updated.Status = state.AlarmOK
			updated.FiredAt = nil
			event = &state.AlarmEvent{
				MonitorName: r.MonitorName,
				Transition:  state.TransitionResolved,
				Message:     r.Message,
				Value:       r.Value,
				FiredAt:     now,
			}
		} else {
			updated.Status = state.AlarmOK
		}
	}

	st.SetAlarm(updated)

	if event != nil {
		st.AppendHistory(state.HistoryEvent{
			MonitorName: r.MonitorName,
			Transition:  event.Transition,
			At:          now,
			Message:     r.Message,
		})
	}

	return event
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func containsStr(value, substr any) bool {
	v := fmt.Sprintf("%v", value)
	s := fmt.Sprintf("%v", substr)
	return len(v) >= len(s) && (v == s || len(s) == 0 ||
		func() bool {
			for i := 0; i <= len(v)-len(s); i++ {
				if v[i:i+len(s)] == s {
					return true
				}
			}
			return false
		}())
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/engine/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/engine/evaluator.go internal/engine/evaluator_test.go
git commit -m "feat: add threshold evaluator"
```

---

### Task 10: Dispatcher

**Files:**
- Create: `internal/engine/dispatcher.go`
- Create: `internal/engine/dispatcher_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/engine/dispatcher_test.go
package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/state"
)

type fakeAction struct {
	name   string
	fired  []state.AlarmEvent
}

func (f *fakeAction) Name() string { return f.name }
func (f *fakeAction) Fire(_ context.Context, ev state.AlarmEvent) error {
	f.fired = append(f.fired, ev)
	return nil
}

func TestDispatcherFiresOnEvent(t *testing.T) {
	fa := &fakeAction{name: "notify"}
	actionMap := map[string]action.Action{"notify": fa}
	monitorActions := map[string][]string{"test": {"notify"}}
	st := state.NewStore()

	d := engine.NewDispatcher(st, actionMap, monitorActions)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 1 {
		t.Fatalf("expected 1 fired event, got %d", len(fa.fired))
	}
}

func TestDispatcherSkipsSilenced(t *testing.T) {
	fa := &fakeAction{name: "notify"}
	actionMap := map[string]action.Action{"notify": fa}
	monitorActions := map[string][]string{"test": {"notify"}}
	st := state.NewStore()
	st.AddSilence(state.Silence{ID: "1", MonitorName: "test", Until: time.Now().Add(time.Hour)})

	d := engine.NewDispatcher(st, actionMap, monitorActions)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 0 {
		t.Fatalf("expected 0 fired events (silenced), got %d", len(fa.fired))
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/engine/... 2>&1 | head -5
```

- [ ] **Step 3: Implement dispatcher.go**

```go
// internal/engine/dispatcher.go
package engine

import (
	"context"
	"log"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/state"
)

type Dispatcher struct {
	store          *state.Store
	actions        map[string]action.Action
	monitorActions map[string][]string // monitorName → []actionName
}

func NewDispatcher(st *state.Store, actions map[string]action.Action, monitorActions map[string][]string) *Dispatcher {
	return &Dispatcher{store: st, actions: actions, monitorActions: monitorActions}
}

func (d *Dispatcher) Dispatch(ctx context.Context, ev state.AlarmEvent) {
	if d.store.IsSilenced(ev.MonitorName) {
		return
	}
	names := d.monitorActions[ev.MonitorName]
	for _, name := range names {
		a, ok := d.actions[name]
		if !ok {
			log.Printf("dispatcher: unknown action %q for monitor %q", name, ev.MonitorName)
			continue
		}
		if err := a.Fire(ctx, ev); err != nil {
			log.Printf("dispatcher: action %q failed for monitor %q: %v", name, ev.MonitorName, err)
		}
	}
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/engine/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/engine/dispatcher.go internal/engine/dispatcher_test.go
git commit -m "feat: add dispatcher with silence support"
```

---

### Task 11: State writer (ConfigMap persistence)

**Files:**
- Create: `internal/engine/statewriter.go`

- [ ] **Step 1: Add k8s dependencies**

```bash
go get k8s.io/client-go@v0.31.0
go get k8s.io/api@v0.31.0
go get k8s.io/apimachinery@v0.31.0
go mod tidy
```

- [ ] **Step 2: Implement statewriter.go**

```go
// internal/engine/statewriter.go
package engine

import (
	"context"
	"encoding/json"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/mfeldheim/klyra/internal/state"
)

const stateWindow = 24 * time.Hour

type StateWriter struct {
	store     *state.Store
	client    kubernetes.Interface
	namespace string
	cmName    string
	interval  time.Duration
}

func NewStateWriter(st *state.Store, client kubernetes.Interface, namespace, cmName string) *StateWriter {
	return &StateWriter{
		store:     st,
		client:    client,
		namespace: namespace,
		cmName:    cmName,
		interval:  10 * time.Second,
	}
}

func (w *StateWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if w.store.IsDirty() {
				if err := w.flush(ctx); err != nil {
					log.Printf("statewriter: flush failed: %v", err)
				} else {
					w.store.ClearDirty()
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (w *StateWriter) flush(ctx context.Context) error {
	ps := w.store.Snapshot(stateWindow)
	data, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	cms := w.client.CoreV1().ConfigMaps(w.namespace)
	existing, err := cms.Get(ctx, w.cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: w.cmName, Namespace: w.namespace},
			Data:       map[string]string{"state.json": string(data)},
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Data = map[string]string{"state.json": string(data)}
	_, err = cms.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// LoadFromConfigMap reads persisted state into the store on startup.
func LoadFromConfigMap(ctx context.Context, st *state.Store, client kubernetes.Interface, namespace, cmName string) error {
	cms := w.client.CoreV1().ConfigMaps(namespace)
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil // first run, nothing to load
	}
	if err != nil {
		return err
	}
	raw, ok := cm.Data["state.json"]
	if !ok {
		return nil
	}
	var ps state.PersistedState
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return err
	}
	st.LoadSnapshot(ps)
	return nil
}
```

- [ ] **Step 3: Fix the bug in LoadFromConfigMap (remove stale `cms` var)**

```go
// Replace the LoadFromConfigMap function body with:
func LoadFromConfigMap(ctx context.Context, st *state.Store, client kubernetes.Interface, namespace, cmName string) error {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, ok := cm.Data["state.json"]
	if !ok {
		return nil
	}
	var ps state.PersistedState
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return err
	}
	st.LoadSnapshot(ps)
	return nil
}
```

- [ ] **Step 4: Verify compile**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/engine/statewriter.go go.mod go.sum
git commit -m "feat: add ConfigMap state writer"
```

---

### Task 12: Engine orchestrator

**Files:**
- Create: `internal/engine/engine.go`

- [ ] **Step 1: Implement engine.go**

```go
// internal/engine/engine.go
package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

type Engine struct {
	cfg        *config.Config
	store      *state.Store
	dispatcher *Dispatcher
	writer     *StateWriter
	k8sClient  kubernetes.Interface
	namespace  string
}

func New(cfg *config.Config, st *state.Store, k8sClient kubernetes.Interface, namespace string) (*Engine, error) {
	actions, monitorActions, err := buildActions(cfg)
	if err != nil {
		return nil, err
	}
	dispatcher := NewDispatcher(st, actions, monitorActions)
	writer := NewStateWriter(st, k8sClient, namespace, "klyra-state")
	return &Engine{
		cfg:        cfg,
		store:      st,
		dispatcher: dispatcher,
		writer:     writer,
		k8sClient:  k8sClient,
		namespace:  namespace,
	}, nil
}

func (e *Engine) Run(ctx context.Context) error {
	results := make(chan state.CheckResult, 100)
	var wg sync.WaitGroup

	// Start monitor goroutines
	for _, mc := range e.cfg.Monitors {
		m, err := monitor.New(mc.Type, mc.Name, mc.Config)
		if err != nil {
			return fmt.Errorf("monitor %q: %w", mc.Name, err)
		}
		interval := mc.Interval.Duration
		if interval == 0 {
			interval = 30 * time.Second
		}
		wg.Add(1)
		go func(m monitor.Monitor, interval time.Duration, thr config.ThresholdConfig) {
			defer wg.Done()
			monitor.Run(ctx, m, interval, results)
		}(m, interval, mc.Threshold)
	}

	// Build threshold map
	thresholds := make(map[string]config.ThresholdConfig, len(e.cfg.Monitors))
	for _, mc := range e.cfg.Monitors {
		thresholds[mc.Name] = mc.Threshold
	}

	// Start state writer
	go e.writer.Run(ctx)

	// Evaluate loop
	go func() {
		for {
			select {
			case r := <-results:
				thr, ok := thresholds[r.MonitorName]
				if !ok {
					continue
				}
				if ev := ApplyResult(e.store, thr, r); ev != nil {
					e.dispatcher.Dispatch(ctx, *ev)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

func buildActions(cfg *config.Config) (map[string]action.Action, map[string][]string, error) {
	actions := make(map[string]action.Action, len(cfg.Actions))
	for _, ac := range cfg.Actions {
		a, err := action.New(ac.Type, ac.Name, ac.Config)
		if err != nil {
			return nil, nil, fmt.Errorf("action %q: %w", ac.Name, err)
		}
		actions[ac.Name] = a
	}
	monitorActions := make(map[string][]string, len(cfg.Monitors))
	for _, mc := range cfg.Monitors {
		monitorActions[mc.Name] = mc.Actions
	}
	return actions, monitorActions, nil
}
```

- [ ] **Step 2: Add missing time import in engine.go**

```go
// Add to imports:
"time"
```

- [ ] **Step 3: Verify compile**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/engine/engine.go
git commit -m "feat: add engine orchestrator"
```

---

*Section 2 complete. Continuing in Section 3 — Monitor modules.*

---

## Section 3 — Monitor Modules (HTTP, Prometheus, Kubernetes)

---

### Task 13: HTTP monitor module

**Files:**
- Create: `internal/monitor/http/http.go`
- Create: `internal/monitor/http/http_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/monitor/http/http_test.go
package httpmon_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	httpmon "github.com/mfeldheim/klyra/internal/monitor/http"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestHTTPMonitorOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"method":        "GET",
		"expect_status": float64(200),
		"expect_body":   "ok",
		"timeout":       "5s",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v", r.Value)
	}
}

func TestHTTPMonitorFailsOnWrongStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"expect_status": float64(200),
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != false {
		t.Errorf("expected value false, got %v", r.Value)
	}
}

func TestHTTPMonitorFailsOnMissingBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("healthy"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"expect_status": float64(200),
		"expect_body":   "ok",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, _ := m.Check(context.Background())
	if r.Value != false {
		t.Errorf("expected false when body mismatch, got %v", r.Value)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/monitor/http/... 2>&1 | head -5
```

- [ ] **Step 3: Implement http.go**

```go
// internal/monitor/http/http.go
package httpmon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("http", New)
}

type HTTPMonitor struct {
	name         string
	url          string
	method       string
	timeout      time.Duration
	expectStatus int
	expectBody   string
	headers      map[string]string
	client       *http.Client
}

func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	url, _ := cfg["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("http monitor %q: url is required", name)
	}
	method := stringOrDefault(cfg, "method", "GET")
	timeoutStr := stringOrDefault(cfg, "timeout", "10s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 10 * time.Second
	}
	expectStatus := 200
	if v, ok := cfg["expect_status"]; ok {
		expectStatus = int(toFloat64(v))
	}
	expectBody, _ := cfg["expect_body"].(string)
	headers := map[string]string{}
	if h, ok := cfg["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}
	return &HTTPMonitor{
		name:         name,
		url:          url,
		method:       method,
		timeout:      timeout,
		expectStatus: expectStatus,
		expectBody:   expectBody,
		headers:      headers,
		client:       &http.Client{Timeout: timeout},
	}, nil
}

func (m *HTTPMonitor) Name() string { return m.name }

func (m *HTTPMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	req, err := http.NewRequestWithContext(ctx, m.method, m.url, nil)
	if err != nil {
		return state.CheckResult{Status: state.CheckUnknown, Message: err.Error()}, nil
	}
	for k, v := range m.headers {
		req.Header.Set(k, v)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckOK,
			Value:       false,
			Message:     err.Error(),
			Timestamp:   time.Now(),
		}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	ok := resp.StatusCode == m.expectStatus
	msg := fmt.Sprintf("status %d", resp.StatusCode)
	if !ok {
		msg = fmt.Sprintf("expected status %d, got %d", m.expectStatus, resp.StatusCode)
	}
	if ok && m.expectBody != "" {
		if !strings.Contains(string(body), m.expectBody) {
			ok = false
			msg = fmt.Sprintf("body missing expected substring %q", m.expectBody)
		}
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       ok,
		Message:     msg,
		Timestamp:   time.Now(),
	}, nil
}

func stringOrDefault(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return def
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/monitor/http/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/monitor/http/
git commit -m "feat: add HTTP monitor module"
```

---

### Task 14: Prometheus monitor module

**Files:**
- Create: `internal/monitor/prometheus/prometheus.go`
- Create: `internal/monitor/prometheus/prometheus_test.go`

- [ ] **Step 1: Add Prometheus client dependency**

```bash
go get github.com/prometheus/client_golang@v1.19.0
go mod tidy
```

- [ ] **Step 2: Write failing test**

```go
// internal/monitor/prometheus/prometheus_test.go
package prommon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	prommon "github.com/mfeldheim/klyra/internal/monitor/prometheus"
	"github.com/mfeldheim/klyra/internal/state"
)

func makePromResponse(value float64) any {
	return map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "scalar",
			"result":     []any{float64(1234567890), fmt.Sprintf("%g", value)},
		},
	}
}

func TestPrometheusMonitorScalar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "scalar",
				"result":     []any{float64(1234567890), "0.042"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m, err := prommon.New("test", map[string]any{
		"url":    srv.URL,
		"query":  `rate(http_errors_total[5m])`,
		"result": "scalar",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s", r.Status)
	}
	val, ok := r.Value.(float64)
	if !ok || val < 0.04 || val > 0.05 {
		t.Errorf("unexpected value: %v", r.Value)
	}
}
```

- [ ] **Step 3: Add missing fmt import to test**

```go
// Add to imports in prometheus_test.go:
"fmt"
```

- [ ] **Step 4: Run — expect compile failure**

```bash
go test ./internal/monitor/prometheus/... 2>&1 | head -5
```

- [ ] **Step 5: Implement prometheus.go**

```go
// internal/monitor/prometheus/prometheus.go
package prommon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("prometheus", New)
}

type PrometheusMonitor struct {
	name       string
	url        string
	query      string
	resultType string // "scalar" or "first_value"
	client     *http.Client
}

func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	rawURL, _ := cfg["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("prometheus monitor %q: url is required", name)
	}
	query, _ := cfg["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("prometheus monitor %q: query is required", name)
	}
	resultType, _ := cfg["result"].(string)
	if resultType == "" {
		resultType = "scalar"
	}
	return &PrometheusMonitor{
		name:       name,
		url:        rawURL,
		query:      query,
		resultType: resultType,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (m *PrometheusMonitor) Name() string { return m.name }

func (m *PrometheusMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	endpoint := m.url + "/api/v1/query"
	params := url.Values{"query": {m.query}}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: time.Now()}, nil
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: time.Now()}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     any    `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: "invalid response"}, nil
	}
	if envelope.Status != "success" {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: "prometheus returned non-success"}, nil
	}

	val, err := extractValue(envelope.Data.Result, m.resultType)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: time.Now()}, nil
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       val,
		Message:     fmt.Sprintf("%.6g", val),
		Timestamp:   time.Now(),
	}, nil
}

func extractValue(result any, resultType string) (float64, error) {
	switch resultType {
	case "scalar":
		arr, ok := result.([]any)
		if !ok || len(arr) < 2 {
			return 0, fmt.Errorf("unexpected scalar format")
		}
		return parsePromValue(arr[1])
	case "first_value":
		arr, ok := result.([]any)
		if !ok || len(arr) == 0 {
			return 0, fmt.Errorf("empty vector result")
		}
		item, ok := arr[0].(map[string]any)
		if !ok {
			return 0, fmt.Errorf("unexpected vector item format")
		}
		vals, ok := item["value"].([]any)
		if !ok || len(vals) < 2 {
			return 0, fmt.Errorf("unexpected value format")
		}
		return parsePromValue(vals[1])
	}
	return 0, fmt.Errorf("unknown result type %q", resultType)
}

func parsePromValue(v any) (float64, error) {
	switch s := v.(type) {
	case string:
		return strconv.ParseFloat(s, 64)
	case float64:
		return s, nil
	}
	return 0, fmt.Errorf("cannot parse value %v", v)
}
```

- [ ] **Step 6: Run tests — expect pass**

```bash
go test ./internal/monitor/prometheus/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/monitor/prometheus/ go.mod go.sum
git commit -m "feat: add Prometheus monitor module"
```

---

### Task 15: Kubernetes monitor module

**Files:**
- Create: `internal/monitor/kubernetes/kubernetes.go`
- Create: `internal/monitor/kubernetes/kubernetes_test.go`

- [ ] **Step 1: Write failing test using fake k8s client**

```go
// internal/monitor/kubernetes/kubernetes_test.go
package k8smon_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	k8smon "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestDeploymentReadyReplicas(t *testing.T) {
	client := fake.NewSimpleClientset()
	ready := int32(2)
	replicas := int32(3)
	client.AppsV1().Deployments("default").Create(context.Background(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: ready,
			Replicas:      replicas,
		},
	}, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":      "deployment",
		"namespace": "default",
		"name":      "api",
		"check":     "ready_replicas",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != float64(2) {
		t.Errorf("expected ready_replicas=2, got %v", r.Value)
	}
}

func TestNodeReadyCondition(t *testing.T) {
	client := fake.NewSimpleClientset()
	corev1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue},
			},
		},
	}
	client.CoreV1().Nodes().Create(context.Background(), corev1, metav1.CreateOptions{})

	m, err := k8smon.NewWithClient("test", map[string]any{
		"kind":  "node",
		"name":  "node-1",
		"check": "ready_condition",
	}, client)
	if err != nil {
		t.Fatal(err)
	}

	r, _ := m.Check(context.Background())
	if r.Value != true {
		t.Errorf("expected ready_condition=true, got %v", r.Value)
	}
}
```

- [ ] **Step 2: Fix missing imports in test**

```go
// Add to imports:
v1 "k8s.io/api/core/v1"
```

- [ ] **Step 3: Run — expect compile failure**

```bash
go test ./internal/monitor/kubernetes/... 2>&1 | head -5
```

- [ ] **Step 4: Implement kubernetes.go**

```go
// internal/monitor/kubernetes/kubernetes.go
package k8smon

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("kubernetes", New)
}

type K8sMonitor struct {
	name      string
	kind      string
	namespace string
	resName   string
	check     string
	client    kubernetes.Interface
}

// New creates a K8sMonitor using the in-cluster or kubeconfig client.
// The client is injected at engine startup via NewWithClient.
func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	return nil, fmt.Errorf("kubernetes monitor requires a pre-built client; use NewWithClient")
}

func NewWithClient(name string, cfg map[string]any, client kubernetes.Interface) (monitor.Monitor, error) {
	kind, _ := cfg["kind"].(string)
	if kind == "" {
		return nil, fmt.Errorf("kubernetes monitor %q: kind is required", name)
	}
	check, _ := cfg["check"].(string)
	if check == "" {
		return nil, fmt.Errorf("kubernetes monitor %q: check is required", name)
	}
	ns, _ := cfg["namespace"].(string)
	resName, _ := cfg["name"].(string)
	return &K8sMonitor{
		name:      name,
		kind:      kind,
		namespace: ns,
		resName:   resName,
		check:     check,
		client:    client,
	}, nil
}

func (m *K8sMonitor) Name() string { return m.name }

func (m *K8sMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()
	val, msg, err := m.fetch(ctx)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: now}, nil
	}
	return state.CheckResult{MonitorName: m.name, Status: state.CheckOK, Value: val, Message: msg, Timestamp: now}, nil
}

func (m *K8sMonitor) fetch(ctx context.Context) (any, string, error) {
	switch m.kind {
	case "deployment":
		return m.checkDeployment(ctx)
	case "pod":
		return m.checkPod(ctx)
	case "node":
		return m.checkNode(ctx)
	case "event":
		return m.checkEvent(ctx)
	}
	return nil, "", fmt.Errorf("unknown kind %q", m.kind)
}

func (m *K8sMonitor) checkDeployment(ctx context.Context) (any, string, error) {
	d, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, m.resName, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	switch m.check {
	case "ready_replicas":
		v := float64(d.Status.ReadyReplicas)
		return v, fmt.Sprintf("%d ready", d.Status.ReadyReplicas), nil
	case "available_replicas":
		v := float64(d.Status.AvailableReplicas)
		return v, fmt.Sprintf("%d available", d.Status.AvailableReplicas), nil
	case "paused":
		return d.Spec.Paused, fmt.Sprintf("paused=%v", d.Spec.Paused), nil
	}
	return nil, "", fmt.Errorf("unknown check %q for deployment", m.check)
}

func (m *K8sMonitor) checkPod(ctx context.Context) (any, string, error) {
	pod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, m.resName, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	switch m.check {
	case "phase":
		return string(pod.Status.Phase), string(pod.Status.Phase), nil
	case "restarts":
		var total int32
		for _, cs := range pod.Status.ContainerStatuses {
			total += cs.RestartCount
		}
		return float64(total), fmt.Sprintf("%d restarts", total), nil
	case "ready_condition":
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady {
				return c.Status == corev1.ConditionTrue, string(c.Status), nil
			}
		}
		return false, "no Ready condition", nil
	}
	return nil, "", fmt.Errorf("unknown check %q for pod", m.check)
}

func (m *K8sMonitor) checkNode(ctx context.Context) (any, string, error) {
	var nodes []corev1.Node
	if m.resName != "" {
		n, err := m.client.CoreV1().Nodes().Get(ctx, m.resName, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		nodes = []corev1.Node{*n}
	} else {
		list, err := m.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, "", err
		}
		nodes = list.Items
	}

	for _, node := range nodes {
		for _, c := range node.Status.Conditions {
			switch m.check {
			case "ready_condition":
				if c.Type == corev1.NodeReady {
					if c.Status != corev1.ConditionTrue {
						return false, fmt.Sprintf("node %s not ready", node.Name), nil
					}
				}
			case "disk_pressure":
				if c.Type == corev1.NodeDiskPressure && c.Status == corev1.ConditionTrue {
					return true, fmt.Sprintf("node %s has disk pressure", node.Name), nil
				}
			case "memory_pressure":
				if c.Type == corev1.NodeMemoryPressure && c.Status == corev1.ConditionTrue {
					return true, fmt.Sprintf("node %s has memory pressure", node.Name), nil
				}
			}
		}
	}

	switch m.check {
	case "ready_condition":
		return true, "all nodes ready", nil
	case "disk_pressure", "memory_pressure":
		return false, "no pressure", nil
	}
	return nil, "", fmt.Errorf("unknown check %q for node", m.check)
}

func (m *K8sMonitor) checkEvent(ctx context.Context) (any, string, error) {
	list, err := m.client.CoreV1().Events(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, "", err
	}
	reason, _ := m.resName, "" // resName reused as reason pattern for events
	for _, ev := range list.Items {
		if reason != "" && ev.Reason != reason {
			continue
		}
		if m.check == "type=Warning" && ev.Type != "Warning" {
			continue
		}
		return true, fmt.Sprintf("event: %s %s", ev.Reason, ev.Message), nil
	}
	return false, "no matching events", nil
}
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./internal/monitor/kubernetes/... -v
```

- [ ] **Step 6: Update engine.go to inject k8s client into kubernetes monitors**

In `internal/engine/engine.go`, replace the monitor construction loop to handle the kubernetes type specially:

```go
// Replace the monitor goroutine section in Run():
for _, mc := range e.cfg.Monitors {
    var m monitor.Monitor
    var err error
    if mc.Type == "kubernetes" {
        m, err = k8smon.NewWithClient(mc.Name, mc.Config, e.k8sClient)
    } else {
        m, err = monitor.New(mc.Type, mc.Name, mc.Config)
    }
    if err != nil {
        return fmt.Errorf("monitor %q: %w", mc.Name, err)
    }
    // ... rest unchanged
}
```

Add import at top of engine.go:
```go
k8smon "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
```

- [ ] **Step 7: Verify compile**

```bash
go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add internal/monitor/kubernetes/ internal/engine/engine.go
git commit -m "feat: add Kubernetes monitor module"
```

---

*Section 3 complete. Continuing in Section 4 — Action module, leader election, HTTP server, CLI.*

---

## Section 4 — HTTP Action, Leader Election, HTTP Server, CLI

---

### Task 16: HTTP action module (ntfy.sh compatible)

**Files:**
- Create: `internal/action/http/http.go`
- Create: `internal/action/http/http_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/action/http/http_test.go
package httpaction_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpaction "github.com/mfeldheim/klyra/internal/action/http"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestHTTPActionFiresWithNtfyHeaders(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	a, err := httpaction.New("notify", map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"auth": map[string]any{
			"type":  "bearer",
			"token": "mytoken",
		},
		"ntfy": map[string]any{
			"priority": "high",
			"tags":     []any{"warning", "k8s"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	ev := state.AlarmEvent{
		MonitorName: "test-monitor",
		Transition:  state.TransitionFiring,
		Message:     "replica count low",
		Value:       float64(1),
		FiredAt:     now,
	}
	if err := a.Fire(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if gotReq.Header.Get("Authorization") != "Bearer mytoken" {
		t.Errorf("expected Bearer auth, got %s", gotReq.Header.Get("Authorization"))
	}
	if gotReq.Header.Get("X-Priority") != "high" {
		t.Errorf("expected X-Priority high, got %s", gotReq.Header.Get("X-Priority"))
	}
	if gotReq.Header.Get("X-Tags") != "warning,k8s" {
		t.Errorf("expected X-Tags warning,k8s, got %s", gotReq.Header.Get("X-Tags"))
	}
	if gotReq.Header.Get("X-Title") != "test-monitor" {
		t.Errorf("expected X-Title test-monitor, got %s", gotReq.Header.Get("X-Title"))
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if payload["status"] != "FIRING" {
		t.Errorf("expected status FIRING, got %v", payload["status"])
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/action/http/... 2>&1 | head -5
```

- [ ] **Step 3: Implement http.go**

```go
// internal/action/http/http.go
package httpaction

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	action.Register("http", New)
}

type HTTPAction struct {
	name     string
	url      string
	method   string
	token    string
	priority string
	tags     []string
	client   *http.Client
}

func New(name string, cfg map[string]any) (action.Action, error) {
	rawURL, _ := cfg["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("http action %q: url is required", name)
	}
	method := "POST"
	if m, ok := cfg["method"].(string); ok && m != "" {
		method = m
	}
	var token string
	if auth, ok := cfg["auth"].(map[string]any); ok {
		token, _ = auth["token"].(string)
	}
	var priority string
	var tags []string
	if ntfy, ok := cfg["ntfy"].(map[string]any); ok {
		priority, _ = ntfy["priority"].(string)
		if rawTags, ok := ntfy["tags"].([]any); ok {
			for _, t := range rawTags {
				tags = append(tags, fmt.Sprintf("%v", t))
			}
		}
	}
	return &HTTPAction{
		name:     name,
		url:      rawURL,
		method:   method,
		token:    token,
		priority: priority,
		tags:     tags,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *HTTPAction) Name() string { return a.name }

func (a *HTTPAction) Fire(ctx context.Context, ev state.AlarmEvent) error {
	payload := map[string]any{
		"monitor":  ev.MonitorName,
		"status":   string(ev.Transition),
		"message":  ev.Message,
		"value":    fmt.Sprintf("%v", ev.Value),
		"fired_at": ev.FiredAt.Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, a.method, a.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if a.priority != "" {
		req.Header.Set("X-Priority", a.priority)
	}
	if len(a.tags) > 0 {
		req.Header.Set("X-Tags", strings.Join(a.tags, ","))
	}
	req.Header.Set("X-Title", ev.MonitorName)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("action %q: server returned %d", a.name, resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/action/http/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/action/http/
git commit -m "feat: add HTTP action module with ntfy.sh support"
```

---

### Task 17: Leader election

**Files:**
- Create: `internal/leader/election.go`

- [ ] **Step 1: Implement election.go**

```go
// internal/leader/election.go
package leader

import (
	"context"
	"log"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Run acquires a Kubernetes Lease lock and calls onStartedLeading while the
// pod holds the lease. onStoppedLeading is called when the lease is lost.
// Returns when ctx is cancelled.
func Run(ctx context.Context, client kubernetes.Interface, namespace, leaseName string,
	onStartedLeading func(ctx context.Context),
	onStoppedLeading func(),
) {
	id, _ := os.Hostname()

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: onStartedLeading,
			OnStoppedLeading: func() {
				log.Println("leader: lost lease")
				onStoppedLeading()
			},
			OnNewLeader: func(identity string) {
				if identity != id {
					log.Printf("leader: current leader is %s", identity)
				}
			},
		},
	})
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/leader/election.go
git commit -m "feat: add leader election via Kubernetes Lease"
```

---

### Task 18: HTTP API handlers

**Files:**
- Create: `internal/server/handlers.go`
- Create: `internal/server/handlers_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/server/handlers_test.go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/server"
	"github.com/mfeldheim/klyra/internal/state"
)

func makeStore() *state.Store {
	st := state.NewStore()
	st.SetAlarm(state.AlarmState{MonitorName: "test", Status: state.AlarmOK, LastCheck: time.Now()})
	return st
}

func makeCfg() *config.Config {
	return &config.Config{
		Monitors: []config.MonitorConfig{{Name: "test", Type: "http"}},
	}
}

func TestStatusHandler(t *testing.T) {
	h := server.NewHandlers(makeStore(), makeCfg())
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["alarms"]; !ok {
		t.Error("expected alarms key in response")
	}
}

func TestHistoryHandler(t *testing.T) {
	st := makeStore()
	st.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionFiring, At: time.Now()})
	h := server.NewHandlers(st, makeCfg())
	req := httptest.NewRequest("GET", "/api/history", nil)
	w := httptest.NewRecorder()
	h.History(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []any
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestCreateSilenceHandler(t *testing.T) {
	h := server.NewHandlers(makeStore(), makeCfg())
	body := `{"monitor":"test","duration":"1h","reason":"maintenance"}`
	req := httptest.NewRequest("POST", "/api/silences", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateSilence(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/server/... 2>&1 | head -5
```

- [ ] **Step 3: Implement handlers.go**

```go
// internal/server/handlers.go
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

type Handlers struct {
	store *state.Store
	cfg   *config.Config
}

func NewHandlers(st *state.Store, cfg *config.Config) *Handlers {
	return &Handlers{store: st, cfg: cfg}
}

func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"alarms":    h.store.Alarms(),
		"updatedAt": time.Now(),
	})
}

func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.store.History())
}

func (h *Handlers) Config(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.cfg)
}

func (h *Handlers) Silences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.store.Silences())
}

type createSilenceRequest struct {
	Monitor  string `json:"monitor"`
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

func (h *Handlers) CreateSilence(w http.ResponseWriter, r *http.Request) {
	var req createSilenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	dur, err := time.ParseDuration(req.Duration)
	if err != nil {
		http.Error(w, "invalid duration", http.StatusBadRequest)
		return
	}
	sl := state.Silence{
		ID:          uuid.NewString(),
		MonitorName: req.Monitor,
		Until:       time.Now().Add(dur),
		Reason:      req.Reason,
	}
	h.store.AddSilence(sl)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sl)
}

func (h *Handlers) DeleteSilence(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: DELETE /api/silences/{id}
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
```

- [ ] **Step 4: Add uuid dependency**

```bash
go get github.com/google/uuid
go mod tidy
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./internal/server/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_test.go go.mod go.sum
git commit -m "feat: add HTTP API handlers"
```

---

### Task 19: HTTP server with SPA fallback

**Files:**
- Create: `internal/server/server.go`

- [ ] **Step 1: Implement server.go**

```go
// internal/server/server.go
package server

import (
	"io/fs"
	"log"
	"net/http"
)

type Server struct {
	handlers *Handlers
	uiFS     fs.FS // nil until UI is embedded
}

func New(h *Handlers, uiFS fs.FS) *Server {
	return &Server{handlers: h, uiFS: uiFS}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", s.handlers.Status)
	mux.HandleFunc("/api/history", s.handlers.History)
	mux.HandleFunc("/api/config", s.handlers.Config)
	mux.HandleFunc("/api/silences", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.Silences(w, r)
		case http.MethodPost:
			s.handlers.CreateSilence(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/silences/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			s.handlers.DeleteSilence(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	if s.uiFS != nil {
		fileServer := http.FileServer(http.FS(s.uiFS))
		mux.Handle("/", spaHandler{fileServer: fileServer, uiFS: s.uiFS})
	}

	return mux
}

func (s *Server) ListenAndServe(addr string) error {
	log.Printf("server: listening on %s", addr)
	return http.ListenAndServe(addr, s.Handler())
}

// spaHandler serves static files and falls back to index.html for SPA routing.
type spaHandler struct {
	fileServer http.Handler
	uiFS       fs.FS
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, err := h.uiFS.Open(r.URL.Path)
	if err != nil {
		// File not found — serve index.html for client-side routing
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		h.fileServer.ServeHTTP(w, r2)
		return
	}
	h.fileServer.ServeHTTP(w, r)
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/server.go
git commit -m "feat: add HTTP server with SPA fallback"
```

---

### Task 20: CLI entrypoint (cobra)

**Files:**
- Create: `cmd/root.go`
- Update: `main.go`

- [ ] **Step 1: Add cobra dependency**

```bash
go get github.com/spf13/cobra
go mod tidy
```

- [ ] **Step 2: Implement cmd/root.go**

```go
// cmd/root.go
package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	_ "github.com/mfeldheim/klyra/internal/action/http"
	_ "github.com/mfeldheim/klyra/internal/monitor/http"
	_ "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	_ "github.com/mfeldheim/klyra/internal/monitor/prometheus"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/leader"
	"github.com/mfeldheim/klyra/internal/server"
	"github.com/mfeldheim/klyra/internal/state"
)

var (
	flagConfigPath string
	flagAddr       string
	flagNamespace  string
	flagLeaseName  string
	flagKubeconfig string
)

var rootCmd = &cobra.Command{
	Use:   "klyra",
	Short: "Kubernetes monitoring tool",
	RunE:  run,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&flagConfigPath, "config", "/etc/klyra/config.yaml", "path to config file")
	rootCmd.Flags().StringVar(&flagAddr, "addr", ":8080", "HTTP listen address")
	rootCmd.Flags().StringVar(&flagNamespace, "namespace", "default", "Kubernetes namespace")
	rootCmd.Flags().StringVar(&flagLeaseName, "lease-name", "klyra-leader", "leader election lease name")
	rootCmd.Flags().StringVar(&flagKubeconfig, "kubeconfig", "", "path to kubeconfig (empty = in-cluster)")
}

func run(cmd *cobra.Command, args []string) error {
	f, err := os.Open(flagConfigPath)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg, err := config.Load(f)
	if err != nil {
		return err
	}

	k8sClient, err := buildK8sClient(flagKubeconfig)
	if err != nil {
		return err
	}

	st := state.NewStore()

	// Load persisted state from ConfigMap on startup
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := engine.LoadFromConfigMap(ctx, st, k8sClient, flagNamespace, "klyra-state"); err != nil {
		log.Printf("warning: could not load persisted state: %v", err)
	}

	// Start HTTP server (all replicas)
	h := server.NewHandlers(st, cfg)
	srv := server.New(h, nil) // UI fs injected at build time via embed.go
	go func() {
		if err := srv.ListenAndServe(flagAddr); err != nil {
			log.Printf("server error: %v", err)
		}
	}()

	// Leader election — only leader runs engine
	eng, err := engine.New(cfg, st, k8sClient, flagNamespace)
	if err != nil {
		return err
	}

	leader.Run(ctx, k8sClient, flagNamespace, flagLeaseName,
		func(leaderCtx context.Context) {
			log.Println("leader: starting engine")
			if err := eng.Run(leaderCtx); err != nil {
				log.Printf("engine error: %v", err)
			}
		},
		func() { log.Println("leader: engine stopped") },
	)

	return nil
}

func buildK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	var restCfg *rest.Config
	var err error
	if kubeconfig != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add cmd/root.go go.mod go.sum
git commit -m "feat: add CLI entrypoint with leader election and engine wiring"
```

---

*Section 4 complete. Continuing in Section 5 — React UI.*
