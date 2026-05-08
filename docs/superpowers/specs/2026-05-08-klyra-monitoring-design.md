# Klyra — Kubernetes Monitoring Tool Design

**Date:** 2026-05-08  
**Status:** Approved

---

## Overview

Klyra is a Kubernetes-native monitoring tool written in Go. It runs as a single Deployment with multiple replicas, evaluates config-driven monitors on independent intervals, and dispatches HTTP actions when alarm thresholds are crossed. A React UI provides a live status dashboard, 24h history, config viewer, and silence management.

---

## Architecture

### Deployment model

- Single Kubernetes `Deployment`, multiple replicas
- **Leader election** via Kubernetes Lease — only the leader runs the monitoring engine and writes state; all replicas serve the HTTP/UI
- Non-leader replicas read `klyra-state` ConfigMap every 30s to serve current status to the UI

### State storage

- `klyra-config` ConfigMap — YAML monitor/action definitions, loaded at startup (restart-to-reload)
- `klyra-state` ConfigMap — JSON alarm states + 24h event history, written by the leader every 10s (only when dirty), trimmed to a 24h rolling window
- No external dependencies (no Redis, no database)
- Size budget: ~50 monitors × ~200 bytes state + history events ≈ well under the 1MB ConfigMap limit

### Data flow

```
Monitor goroutine → CheckResult → results channel
  → Evaluator (threshold + for-duration logic)
    → on state change: Dispatcher → Action.Fire()
    → always: StateWriter (batched, 10s, dirty-only)
```

---

## Configuration

Config lives in `klyra-config` ConfigMap as YAML. `${ENV_VAR}` interpolation is supported for secrets.

### Monitor definition

```yaml
monitors:
  - name: <string>            # unique identifier
    type: kubernetes|http|prometheus
    interval: <duration>      # e.g. 30s, 1m
    config: <module-specific> # see below
    threshold:
      operator: lt|gt|lte|gte|eq|neq|contains|matches
      value: <number|bool|string>
      for: <duration>         # optional: condition must hold this long before firing; omit to fire immediately
    actions:
      - <action-name>         # references actions list by name
```

### Action definition

```yaml
actions:
  - name: <string>
    type: http
    config:
      url: <string>
      method: GET|POST
      auth:
        type: bearer
        token: <string>       # supports ${ENV_VAR}
      ntfy:                   # optional ntfy.sh-compatible headers
        priority: urgent|high|default|low|min
        tags: [<string>, ...]
```

---

## Monitor Modules

### `kubernetes`

Checks Kubernetes resource state via the k8s API.

| `kind`       | `check`                                          |
|--------------|--------------------------------------------------|
| `deployment` | `ready_replicas`, `available_replicas`, `paused` |
| `pod`        | `phase`, `restarts`, `ready_condition`           |
| `node`       | `ready_condition`, `disk_pressure`, `memory_pressure` |
| `event`      | `reason` (string/regex match), `type=Warning`    |

Config fields: `kind`, `namespace` (optional), `name` (optional, omit to match all), `check`.

### `http`

Performs an HTTP request and checks the response.

Config fields: `url`, `method`, `timeout`, `expect_status` (int), `expect_body` (substring), `headers` (map).  
The check produces `true` (all expectations met) or `false`. Threshold is typically `operator: eq, value: false`.

### `prometheus`

Queries a Prometheus HTTP API with a PromQL expression.

Config fields: `url` (Prometheus base URL), `query` (PromQL), `result: scalar|first_value`.  
Result is a float64 applied directly to the threshold operator.

---

## Action Modules

### `http`

Posts an HTTP request when an alarm fires or resolves. Supports ntfy.sh-compatible auth and headers.

**Request body** (JSON):
```json
{
  "monitor": "<name>",
  "status": "FIRING|RESOLVED",
  "message": "<human readable>",
  "value": "<current value>",
  "fired_at": "<RFC3339>"
}
```

**ntfy.sh headers** (when `ntfy` block is present):
- `Authorization: Bearer <token>`
- `X-Priority: <priority>`
- `X-Tags: <comma-separated tags>`
- `X-Title: <monitor name>`

---

## Internal Go Design

### Interfaces

```go
type Monitor interface {
    Name()  string
    Check(ctx context.Context) (CheckResult, error)
}

type Action interface {
    Name() string
    Fire(ctx context.Context, event AlarmEvent) error
}
```

### Key types

```go
type CheckResult struct {
    MonitorName string
    Status      CheckStatus  // OK | ERROR | UNKNOWN
    Value       any
    Message     string
    Timestamp   time.Time
}

type AlarmState struct {
    MonitorName  string
    Status       AlarmStatus  // OK | FIRING | UNKNOWN
    LastCheck    time.Time
    FiredAt      *time.Time
    PendingSince *time.Time   // for "for:" duration tracking
    LastValue    any
    Message      string
}

type HistoryEvent struct {
    MonitorName string
    Transition  Transition   // FIRING | RESOLVED
    At          time.Time
    Message     string
}

type AlarmEvent struct {
    MonitorName string
    Transition  Transition
    Message     string
    Value       any
    FiredAt     time.Time
}

type Silence struct {
    ID          string
    MonitorName string     // exact match; empty = silence all
    Until       time.Time
    Reason      string
}

type PersistedState struct {
    UpdatedAt time.Time
    Alarms    map[string]AlarmState
    History   []HistoryEvent
    Silences  []Silence
}
```

### Evaluator logic

1. Receive `CheckResult` from results channel
2. Evaluate threshold operator against `result.Value`
3. If threshold met and no `PendingSince` → set `PendingSince = now`
4. If threshold met and `now - PendingSince >= for` → transition to FIRING
5. If threshold not met → clear `PendingSince`, transition to OK if previously FIRING
6. On any state transition → append `HistoryEvent`, call `Dispatcher` (Dispatcher checks active silences before firing actions — silenced monitors skip action dispatch but still record state transitions)
7. Mark state dirty for next `StateWriter` flush

### Module registration

New modules implement the interface and register a factory function:

```go
// internal/monitor/registry.go
var monitorFactories = map[string]MonitorFactory{
    "kubernetes":  kubernetes.New,
    "http":        httpmon.New,
    "prometheus":  prommon.New,
}

// internal/action/registry.go
var actionFactories = map[string]ActionFactory{
    "http": httpaction.New,
}
```

---

## Project Structure

```
klyra/
├── main.go
├── go.mod
├── cmd/
│   └── root.go                    # cobra CLI, flags
├── internal/
│   ├── config/
│   │   ├── loader.go              # parse YAML from ConfigMap
│   │   └── types.go               # MonitorConfig, ActionConfig structs
│   ├── engine/
│   │   ├── engine.go              # orchestrates monitors, evaluator, dispatcher
│   │   ├── evaluator.go           # threshold + for-duration logic
│   │   ├── dispatcher.go          # fires actions on state transition
│   │   └── statewriter.go         # batched ConfigMap writes
│   ├── monitor/
│   │   ├── monitor.go             # Monitor interface + CheckResult
│   │   ├── runner.go              # per-monitor goroutine loop
│   │   ├── registry.go            # factory map
│   │   ├── kubernetes/kubernetes.go
│   │   ├── http/http.go
│   │   └── prometheus/prometheus.go
│   ├── action/
│   │   ├── action.go              # Action interface + AlarmEvent
│   │   ├── registry.go
│   │   └── http/http.go           # ntfy.sh-compatible HTTP action
│   ├── state/
│   │   ├── state.go               # AlarmState, HistoryEvent, Silence types
│   │   └── store.go               # in-memory store, read/write ConfigMap
│   ├── server/
│   │   ├── server.go              # HTTP server, routes, SPA handler
│   │   ├── handlers.go            # /api/* handlers
│   │   └── middleware.go
│   └── leader/
│       └── election.go            # k8s lease-based leader election
├── ui/                            # React + Vite
│   ├── package.json
│   ├── vite.config.ts
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── pages/
│       │   ├── Dashboard.tsx
│       │   ├── History.tsx
│       │   ├── Config.tsx
│       │   └── Silences.tsx
│       ├── components/
│       │   ├── AlarmCard.tsx
│       │   ├── StatusBadge.tsx
│       │   └── Timeline.tsx
│       └── api/
│           └── client.ts
├── deploy/
│   ├── helm/
│   │   └── klyra/
│   │       ├── Chart.yaml
│   │       ├── values.yaml
│   │       └── templates/
│   │           ├── deployment.yaml
│   │           ├── service.yaml
│   │           ├── rbac.yaml
│   │           ├── configmap-config.yaml
│   │           ├── configmap-state.yaml
│   │           ├── lease.yaml
│   │           └── _helpers.tpl
│   └── raw/                       # plain manifests for reference
│       ├── deployment.yaml
│       ├── service.yaml
│       └── rbac.yaml
├── .github/
│   └── workflows/
│       └── ci.yaml
├── Dockerfile                     # multi-stage: build UI → embed → build Go binary
└── Makefile
```

---

## HTTP API

All replicas serve these endpoints. State is read from `klyra-state` ConfigMap.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/status` | Current alarm states for all monitors |
| `GET` | `/api/history` | 24h event timeline (all transitions) |
| `GET` | `/api/config` | Parsed monitor config (read-only) |
| `POST` | `/api/silences` | Create a silence (body: `{monitor, duration, reason}`) |
| `DELETE` | `/api/silences/:id` | Remove a silence |
| `GET` | `/` | React SPA (served from embedded filesystem) |

---

## React UI

Single-page app built with Vite, embedded into the Go binary via `go:embed`.

**Dashboard** — summary bar (firing / ok / unknown counts), 24h mini-timeline per monitor, alarm cards grouped by status (firing first). Each card shows monitor name, type tag, current value, and time firing.

**History** — full 24h event log, filterable by monitor. Shows each FIRING/RESOLVED transition with timestamp and value.

**Config** — read-only view of parsed monitor and action definitions.

**Silences** — list active silences, create new silence (monitor name + duration + reason), delete existing.

UI polls `/api/status` every 30s.

---

## Kubernetes RBAC

The ServiceAccount needs:

- `get`, `list`, `watch` on `pods`, `nodes`, `deployments`, `events` (for k8s monitor module)
- `get`, `create`, `update` on `configmaps` (for `klyra-config` read and `klyra-state` read/write/create-if-missing)
- `get`, `create`, `update` on `leases` (for leader election)

---

## Build & Local Development

```
make build   # build React UI → embed into Go binary
make docker  # build + push multi-arch image (native amd64 + arm64, merged manifest)
make dev     # run locally against current kubeconfig context
```

The Dockerfile uses a multi-stage build: Node stage builds the React UI, Go stage copies the dist output and compiles the binary with `go:embed`. Multi-arch images are built with `docker buildx` targeting `linux/amd64` and `linux/arm64`.

---

## CI/CD

### GitHub Actions — `.github/workflows/ci.yaml`

**Triggers:**
- Push to `main` — builds and pushes image tagged `ghcr.io/mfeldheim/klyra:main` + `ghcr.io/mfeldheim/klyra:<sha>`
- Push of a version tag `v*.*.*` — builds and pushes `ghcr.io/mfeldheim/klyra:<version>` + `ghcr.io/mfeldheim/klyra:latest`, packages and pushes Helm chart to GHCR OCI registry
- Pull requests — build only (no push), run `go test ./...`

**Pipeline stages:**

1. **test** — `go test ./...` + `go vet ./...` (runs on `ubuntu-latest`)
2. **build-ui** — `npm ci && npm run build` in `ui/` (runs on `ubuntu-latest`)
3. **build-amd64** (after test + build-ui) — builds and pushes `ghcr.io/mfeldheim/klyra:<tag>-amd64` on `ubuntu-latest`
4. **build-arm64** (after test + build-ui) — builds and pushes `ghcr.io/mfeldheim/klyra:<tag>-arm64` on `ubuntu-24.04-arm` (GitHub's native ARM runner)
5. **merge-manifest** (after build-amd64 + build-arm64) — uses `docker buildx imagetools create` to combine the two arch-specific images into a single multi-arch manifest at `ghcr.io/mfeldheim/klyra:<tag>`
6. **helm-release** (on version tag only) — `helm package deploy/helm/klyra` + `helm push` to `oci://ghcr.io/mfeldheim/klyra-helm`

Steps 3 and 4 run in parallel on their respective native runners — no QEMU emulation.

**Permissions required in workflow:**
```yaml
permissions:
  contents: read
  packages: write   # push to GHCR
```

Authentication uses `GITHUB_TOKEN` (automatic, no extra secrets needed for GHCR).

### Image tagging strategy

| Event | Tags applied |
|-------|-------------|
| Push to `main` | `main`, `main-<short-sha>` |
| Tag `v1.2.3` | `1.2.3`, `1.2`, `latest` |
| Pull request | build only, no push |

### Helm chart distribution

The Helm chart is published as an OCI artifact alongside the container image:

```
oci://ghcr.io/mfeldheim/klyra-helm
```

Install:
```
helm install klyra oci://ghcr.io/mfeldheim/klyra-helm/klyra --version 1.2.3
```

### Key `values.yaml` fields

```yaml
image:
  repository: ghcr.io/mfeldheim/klyra
  tag: latest
  pullPolicy: IfNotPresent

replicaCount: 2

service:
  type: ClusterIP
  port: 8080

config: {}       # inline klyra-config YAML — merged into ConfigMap

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    memory: 128Mi

leaderElection:
  leaseName: klyra-leader
  namespace: ""  # defaults to release namespace
```
