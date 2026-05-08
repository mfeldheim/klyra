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
