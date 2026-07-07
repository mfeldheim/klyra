# Klyra

Lightweight, config-driven infrastructure monitoring for Kubernetes. Polls user-defined monitors on a schedule, evaluates results against thresholds, and fires notifications on state transitions. A React dashboard is embedded directly in the binary — no external database required. State persists to a Kubernetes ConfigMap.

## Features

- HTTP, Kubernetes API, and Prometheus monitors out of the box
- Threshold-based alerting with `for` / `recovery_for` timing
- Pushover and HTTP webhook (ntfy-compatible) notification actions
- Leader-elected HA deployment (two replicas, one active engine)
- Silence management via UI or API
- 24-hour timeline and event history in the dashboard

## Quick Start

```yaml
# values.yaml (Helm)
config:
  monitors:
    - name: my-site
      type: http
      interval: 30s
      group: web
      config:
        url: https://example.com
        expect_status: 200
      threshold:
        operator: eq
        value: false
        for: 30s
        recovery_for: 60s
      actions: [notify]

  actions:
    - name: notify
      type: pushover
      config:
        token: ${PUSHOVER_TOKEN}
        user: ${PUSHOVER_USER}
        dashboard_url: https://klyra.example.com
```

```bash
helm install klyra oci://ghcr.io/mfeldheim/klyra-helm \
  --version 0.1.27 \
  --namespace klyra \
  --create-namespace \
  --values values.yaml
```

## Monitor Types

### `http`

Polls an HTTP endpoint.

```yaml
config:
  url: https://api.example.com/health   # required
  method: GET                           # default: GET
  timeout: 5s                           # default: 10s
  expect_status: 200                    # legacy shorthand (default: 200)
  expect_body: '"status":"ok"'          # legacy shorthand substring check
  headers:
    Authorization: Bearer ${API_TOKEN}

  # Advanced expressions (all optional). Operators:
  # lt | gt | lte | gte | eq | neq | contains | matches
  status:
    operator: gte
    value: 200

  body:
    operator: contains
    value: healthy

  header:
    name: Content-Type
    operator: contains
    value: application/json

  latency_ms:
    operator: lt
    value: 500

  json:
    path: disk.used_pct                 # dot path; array indexes supported (e.g. items.0.value)
    operator: gt
    value: 90

  # Or use a boolean expression over JSON fields flattened with underscores.
  # Example: payload.system.disk.used becomes payload_system_disk_used
  # json:
  #   expression: "(system_disk_used_bytes / system_disk_total_bytes) * 100 > 90"
```

Returns `Value: bool` — `true` if all configured checks pass.

Example: Typesense disk above 90% (via `/metrics.json`):

```yaml
- name: typesense-disk
  type: http
  interval: 30s
  config:
    url: http://typesense:8108/metrics.json
    json:
      expression: "(system_disk_used_bytes / system_disk_total_bytes) * 100 > 90"
  threshold:
    operator: eq
    value: false
    for: 2m
  actions: [notify]
```

For percentage checks, point `json.path` to a percent field (if present) or use the `prometheus` monitor with a computed query.

---

### `kubernetes`

Queries the Kubernetes API. The `kind` field selects the resource type; `check` selects what to evaluate.

#### `deployment`

```yaml
config:
  kind: deployment
  namespace: production
  name: frontend
  check: ready_replicas    # ready_replicas | available_replicas | paused
```

Returns `Value: float64` (replica count) or `bool` for `paused`.

#### `pod`

```yaml
config:
  kind: pod
  namespace: production
  name: my-pod
  check: restarts          # phase | restarts | ready_condition
```

Returns `Value: string | float64 | bool` depending on check.

#### `node`

```yaml
config:
  kind: node
  name: node-1             # omit to check all nodes
  check: ready_condition   # ready_condition | disk_pressure | memory_pressure
```

Returns `Value: bool`.

#### `event`

Watches Kubernetes events. `name` filters by reason; `check: type=Warning` restricts to Warning events; `window` limits to recent events.

```yaml
config:
  kind: event
  namespace: production
  name: OOMKilling
  check: type=Warning
  window: 10m
```

Returns `Value: bool` — `true` if matching events were found.

#### `pods_ready`

Checks pod Ready conditions across a namespace (or cluster-wide with `namespace: ""`). Skips Succeeded, Failed, and terminating pods. Use `check` as a label selector.

```yaml
config:
  kind: pods_ready
  namespace: production
  check: app=api           # optional label selector
```

Returns `Value: bool` — `true` if any active pod is not Ready.

#### `workloads_ready`

Checks Deployments, StatefulSets, and DaemonSets. A workload is degraded when `readyReplicas < desiredReplicas`. Paused Deployments are skipped.

```yaml
config:
  kind: workloads_ready
  namespace: production    # or "" for cluster-wide
  check: app=api           # optional label selector
```

Returns `Value: bool` — `true` when all workloads are healthy, `false` when any workload is degraded. The message names each degraded workload with namespace and replica count and appends node placement/readiness, e.g. `not ready: deploy/production/api (1/3) nodes=ip-10-0-1-12:Ready,pending:Unscheduled, sts/production/db (0/2) nodes=ip-10-0-2-44:NotReady`.

#### `workloads_zero_ready`

Checks Deployments, StatefulSets, and DaemonSets and reports only workloads with zero ready replicas (`0/<desired>`). Paused Deployments are skipped.

```yaml
config:
  kind: workloads_zero_ready
  namespace: production    # or "" for cluster-wide
  check: app=api           # optional label selector
```

Returns `Value: bool` — `true` when no workload is at `0/<desired>`, `false` when at least one workload is zero-ready.

#### `workloads_partially_ready`

Checks Deployments, StatefulSets, and DaemonSets and reports only partially ready workloads (`<desired-n>/<desired>` where ready is greater than zero). Paused Deployments are skipped.

```yaml
config:
  kind: workloads_partially_ready
  namespace: production    # or "" for cluster-wide
  check: app=api           # optional label selector
```

Returns `Value: bool` — `true` when no workload is partially ready, `false` when at least one workload is partially ready.

---

### `prometheus`

Queries a Prometheus HTTP API.

```yaml
config:
  url: http://prometheus:9090
  query: 'scalar(sum(rate(http_errors_total[5m])))'
  result: scalar    # scalar | first_value
```

Returns `Value: float64`.

---

### `prometheus_scrape`

Fetches a raw Prometheus `/metrics` endpoint directly — no Prometheus server needed.

```yaml
config:
  url: http://my-service:8080/metrics
  metric: my_queue_depth
  result: max            # first | min | max | sum | count (default: first)
  missing_value: 0       # return this value when the metric is absent
```

Returns `Value: float64`. Without `missing_value`, an absent metric returns `UNKNOWN` status.

---

### `cloudwatch`

Queries a CloudWatch metric directly via AWS API.

```yaml
config:
  region: eu-west-1
  namespace: AWS/Kinesis
  metric: GetRecords.IteratorAgeMilliseconds
  stat: Maximum             # Average | Sum | Minimum | Maximum | SampleCount
  period: 300               # seconds (default: 300)
  lookback: 10m             # optional search window (default: 10m)
  dimensions:
    StreamName: yellow-search-sync-production
```

`stream_name` is also supported as shorthand for `dimensions.StreamName`.

Returns `Value: float64`.

---

## Thresholds

Every monitor has a `threshold` block that defines when to fire:

```yaml
threshold:
  operator: gt       # lt | gt | lte | gte | eq | neq | contains | matches
  value: 0
  for: 5m            # optional: condition must hold for this long before FIRING
  recovery_for: 2m   # optional: condition must clear for this long before RESOLVING
```

Without `for`, the alarm fires immediately on the first failing check. Without `recovery_for`, it resolves on the first passing check.

---

## Actions

Actions are referenced by name from each monitor's `actions` list. They fire on `FIRING` and `RESOLVED` transitions.

### `pushover`

```yaml
- name: notify
  type: pushover
  config:
    token: ${PUSHOVER_TOKEN}
    user: ${PUSHOVER_USER}
    dashboard_url: https://klyra.example.com  # optional link in notification
    priority: 1                                # default priority (-1 | 0 | 1 | 2)
```

Monitor-level `priority` overrides the action default on FIRING. Priority `2` (emergency) requires acknowledgement and repeats automatically.

### `http` (webhook / ntfy)

```yaml
- name: ntfy
  type: http
  config:
    url: https://ntfy.sh/my-topic
    method: POST                  # default: POST
    auth:
      token: ${NTFY_TOKEN}
    ntfy:
      priority: high
      tags: [warning, klyra]
```

Posts a JSON body with `monitor`, `status`, `message`, `value`, and `fired_at` fields.

---

## Monitor Fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique monitor name |
| `type` | string | `http`, `kubernetes`, `prometheus`, `prometheus_scrape`, `cloudwatch` |
| `interval` | duration | How often to run (e.g. `30s`, `2m`) |
| `group` | string | Dashboard grouping |
| `icon` | string | Icon name for the dashboard and notifications: `globe`, `kubernetes`, `network`, `tunnel`, `database`, `memory`, `lock`, `warning` |
| `priority` | string | Notification urgency: `low`, `normal`, `high`, `critical` |
| `config` | object | Monitor-type-specific config (see above) |
| `threshold` | object | Alert threshold (see above) |
| `actions` | list | Names of actions to invoke on transitions |

---

## Silences

Silences suppress notifications for a monitor (or all monitors) for a duration.

**Via the UI:** Silences tab → fill in monitor name, duration, reason → Create.

**Via the API:**

```bash
# Silence a specific monitor for 2 hours
curl -X POST http://klyra:8080/api/silences \
  -H 'Content-Type: application/json' \
  -d '{"monitor":"my-site","duration":"2h","reason":"planned maintenance"}'

# Silence all monitors
curl -X POST http://klyra:8080/api/silences \
  -d '{"monitor":"","duration":"30m","reason":"deploy window"}'

# List active silences
curl http://klyra:8080/api/silences

# Remove a silence
curl -X DELETE http://klyra:8080/api/silences/<id>
```

---

## Deployment

The Helm chart creates two replicas with leader election. Only the leader runs the monitoring engine; the standby replica serves the API and syncs state from the ConfigMap.

**Key `values.yaml` options:**

```yaml
image:
  repository: ghcr.io/mfeldheim/klyra
  tag: 0.1.27
  pullPolicy: IfNotPresent

replicaCount: 2

service:
  type: ClusterIP
  port: 8080

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    memory: 128Mi

leaderElection:
  leaseName: klyra-leader

config: {}   # klyra config goes here
```

Environment variables referenced in the config (`${VAR}`) are interpolated at startup from the pod's environment. Inject secrets via `envFrom` or `env` in your Helm values.

### RBAC

The chart provisions:

- A namespaced `Role` for pods, configmaps, and leases.
- `ClusterRole`s for cross-namespace access to pods, nodes, events, and workload resources (deployments, statefulsets, daemonsets).

If you only use namespace-scoped kubernetes monitors, you can disable the cluster-scoped roles by not using `kind: pods_ready` or `kind: workloads_ready` with `namespace: ""`.

---

## Local Development

**Prerequisites:** Go 1.24+, Node 20+, a kubeconfig.

```bash
# Install UI dependencies and build the embedded SPA
make build

# Run tests
make test

# Run locally against your current kubeconfig context
make dev

# Custom config
go run . --config=./my-config.yaml --addr=:8080

# UI hot reload (proxies API to the Go server on :8080)
cd ui && npm install && npm run dev
```

**CLI flags:**

| Flag | Default | Description |
|---|---|---|
| `--config` | `/etc/klyra/config.yaml` | Config file path |
| `--addr` | `:8080` | Listen address |
| `--namespace` | `default` | Namespace for state ConfigMap and leader election lease |
| `--lease-name` | `klyra-leader` | Lease object name |
| `--kubeconfig` | (in-cluster) | Path to kubeconfig file |
