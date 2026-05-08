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
