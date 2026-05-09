# AI Incident Investigation — Design Spec

**Date:** 2026-05-09  
**Status:** Approved

## Overview

When a monitor fires, klyra creates an **incident** — a first-class record with a unique ID, stored as a markdown file in S3. An optional `ai_investigate` action attaches to the incident and immediately launches a Claude Sonnet agent (via AWS Bedrock) with read-only access to the Kubernetes cluster. By the time the on-call engineer opens the Pushover notification, the agent may already have identified the root cause and proposed a fix. The incident detail page shows the investigation as it streams in, and provides an interactive chat for follow-up questions. When the alarm resolves, the incident is closed, the chat session ends, and in-memory conversation history is wiped.

---

## Architecture

### Flow

```
Monitor fires
    │
    ▼
Dispatcher detects FIRING transition
    │
    ├─► IncidentManager.OnFiring(event)
    │       → generates incident ID: inc-{monitor}-{yyyymmdd-hhmmss}-{short-uuid}
    │       → writes incident-{id}.md to S3
    │       → updates index.json in S3
    │       → injects incidentID into AlarmEvent
    │
    ├─► pushover.Fire()
    │       → notification URL: {dashboard_url}/incidents/{id}
    │
    └─► investigate.Fire()  [optional action]
            → spawns goroutine
            → calls Bedrock Converse API with k8s tools
            → streams findings to S3 markdown + in-memory SSE channel
            → updates investigation sub-status as it runs

Monitor resolves
    │
    ▼
Dispatcher detects RESOLVED transition
    │
    └─► IncidentManager.OnResolved(event)
            → marks incident status: resolved in S3
            → wipes conversation history from memory
```

### Request flow (user opens notification)

```
Browser → /incidents/{id}           (SPA route)
        → GET /api/incidents/{id}   (metadata + status from S3)
        → GET /api/incidents/{id}/stream  (SSE: live channel or replay from S3)
        → POST /api/incidents/{id}/chat   (follow-up → SSE response stream)
```

---

## S3 Storage Layout

```
{bucket}/{prefix}/
  index.json                        ← incident list (ID, monitor, firedAt, status)
  incidents/
    inc-{id}/
      incident.md                   ← full incident doc (append-only while active)
```

### index.json schema

```json
{
  "incidents": [
    {
      "id": "inc-api-latency-20260509-143022-a3f1",
      "monitorName": "api-latency",
      "firedAt": "2026-05-09T14:30:22Z",
      "resolvedAt": null,
      "status": "active",
      "investigationStatus": "complete"
    }
  ]
}
```

### incident.md structure

```markdown
# Incident: inc-api-latency-20260509-143022-a3f1

**Monitor:** api-latency  
**Fired At:** 2026-05-09T14:30:22Z  
**Value:** 842ms  
**Message:** p99 latency above threshold  
**Status:** active  
**Investigation:** complete  

---

## Investigation

{streamed markdown from Claude — findings, tool outputs summarised, root cause, proposed fix}

---

## Conversation

### User — 2026-05-09T14:35:10Z
What would happen if I restart the affected pods?

### Assistant — 2026-05-09T14:35:14Z
{response}
```

---

## Incident Statuses

| Field | Values |
|---|---|
| `status` | `active`, `resolved` |
| `investigationStatus` | `pending`, `running`, `complete`, `failed` |

**Lifecycle:**
- Created as `active` / `pending` on FIRING transition
- Investigation moves to `running` when goroutine starts, `complete` or `failed` when it ends
- On RESOLVED: `status` → `resolved`, in-memory conversation history wiped
- After `resolved`: incident detail page is read-only; chat input disabled

---

## New Go Packages

### `internal/incident/`

| File | Responsibility |
|---|---|
| `incident.go` | `Incident` struct, ID generation (`inc-{monitor}-{yyyymmdd-hhmmss}-{shortuuid}`), markdown serialisation |
| `store.go` | `S3Store`: PutObject/GetObject for incident files and `index.json` |
| `manager.go` | `Manager`: `OnFiring`, `OnResolved`; in-memory map of `incidentID → conversationHistory`; exposes `AppendChat(id, msg)` for the chat handler |

### `internal/action/investigate/`

| File | Responsibility |
|---|---|
| `investigate.go` | Implements `action.Action`; `Fire()` calls `manager.StartInvestigation(incidentID, event)` |
| `agent.go` | Bedrock Converse API loop: sends messages, handles `tool_use` blocks, dispatches to tool implementations, streams text deltas |
| `tools.go` | 17 read-only k8s tool implementations (see tool table below) |

---

## Changes to Existing Code

| File | Change |
|---|---|
| `internal/state/state.go` | Add `IncidentID *string` to `AlarmState` |
| `internal/engine/dispatcher.go` | Call `manager.OnFiring` / `manager.OnResolved` before action dispatch; inject `IncidentID` into `AlarmEvent` |
| `internal/action/pushover/pushover.go` | Use `{dashboard_url}/incidents/{id}` as notification URL when `IncidentID` is set |
| `internal/server/server.go` | Register four new routes (see API below) |
| `internal/server/handlers.go` | Implement incident handlers |
| `internal/config/types.go` | Add `IncidentsConfig` (bucket, prefix, region) to `Config` |

---

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/incidents` | List incidents from S3 index |
| `GET` | `/api/incidents/{id}` | Full incident markdown + metadata from S3 |
| `GET` | `/api/incidents/{id}/stream` | SSE: live investigation stream if active, otherwise completed markdown |
| `POST` | `/api/incidents/{id}/chat` | Append user message, resume Bedrock conversation, return SSE stream |

### SSE event types

| Event | Payload |
|---|---|
| `delta` | `{"text": "..."}` — incremental text chunk |
| `status` | `{"investigationStatus": "complete"}` — status change |
| `done` | `{}` — stream closed |

---

## Agent Tool Set

All tools are implemented as Go functions using klyra's existing `kubernetes.Interface` client. Log output is capped at 200 lines. ConfigMap values are never returned — only names — to avoid leaking secrets.

| Tool | Arguments | Returns |
|---|---|---|
| `list_namespaces` | — | all namespace names |
| `list_pods` | `namespace`, `label_selector?` | names, status, restarts, node, age |
| `describe_pod` | `namespace`, `name` | spec + status + conditions |
| `get_pod_logs` | `namespace`, `name`, `container?`, `lines?` (max 200), `previous?` | log lines |
| `list_events` | `namespace`, `involved_object?` | events sorted by time, warnings first |
| `list_deployments` | `namespace` | name, desired/ready/available replicas, conditions |
| `describe_deployment` | `namespace`, `name` | full spec + rollout status |
| `list_replicasets` | `namespace`, `deployment?` | name, desired/ready, owner |
| `list_nodes` | — | name, status, roles, conditions, taints |
| `describe_node` | `name` | allocatable resources, conditions, pressure flags |
| `list_daemonsets` | `namespace` | name, desired/ready/available |
| `list_statefulsets` | `namespace` | name, desired/ready |
| `list_services` | `namespace` | name, type, ClusterIP, ports |
| `list_hpa` | `namespace` | name, target, min/max, current replicas, metrics |
| `get_pod_metrics` | `namespace`, `name` | current CPU + memory usage |
| `list_pod_metrics` | `namespace` | CPU + memory for all pods |
| `list_node_metrics` | — | CPU + memory for all nodes |

---

## Agent System Prompt

```
You are an incident response assistant with read-only access to a Kubernetes cluster.

Monitor "{name}" has fired:
- Type: {type}
- Value: {value}
- Message: {message}
- Fired at: {firedAt}

Use the available tools to investigate. Prefer tool calls over assumptions.
Be systematic: start with the most likely causes given the monitor type and value.

Conclude your investigation with:
1. **Root cause** (or top candidates if uncertain)
2. **Proposed fix** (specific kubectl commands or YAML changes)
3. **Confidence:** high / medium / low
```

---

## Configuration

### klyra.yaml additions

```yaml
incidents:
  s3_bucket: my-klyra-incidents
  s3_prefix: incidents/          # optional, default: "incidents/"
  s3_region: us-east-1

actions:
  - name: investigate
    type: ai_investigate
    config:
      bedrock_region: us-east-1
      model: us.anthropic.claude-sonnet-4-5-v1:0
```

The `incidents` block is required for incident logging. The `ai_investigate` action is optional and listed under a monitor's `actions:` list like any other action. Incident creation requires `incidents` config; AI investigation additionally requires a configured `ai_investigate` action on the monitor.

### Helm chart additions

```yaml
# values.yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/klyra-role
```

Required IAM permissions:
- `s3:PutObject`, `s3:GetObject`, `s3:ListBucket` on the incidents bucket
- `bedrock:InvokeModelWithResponseStream` on the Sonnet model ARN

---

## UI Changes

### New pages

| Component | Route | Description |
|---|---|---|
| `pages/Incidents.tsx` | `incidents` tab | List of all incidents from `/api/incidents`; columns: monitor, fired at, status, investigation status |
| `pages/IncidentDetail.tsx` | `/incidents/:id` | Incident metadata card + streamed investigation report + chat thread |

### New components

| Component | Description |
|---|---|
| `components/InvestigationStream.tsx` | Renders streaming markdown chunks as they arrive via SSE |
| `components/ChatThread.tsx` | Chat input + scrollable message history; disabled when incident is `resolved` |

### Changes to existing UI

| File | Change |
|---|---|
| `App.tsx` | Add `incidents` tab; add `/incidents/:id` client-side route |
| `components/AlarmCard.tsx` | Add "View incident" link when `incidentId` is set on alarm state |

---

## Memory Lifecycle

| Event | Action |
|---|---|
| FIRING transition | `Manager` creates entry in `map[incidentID]conversationHistory` |
| Investigation runs | Tool call/response pairs and Claude messages appended to history |
| RESOLVED transition | Entry deleted from map; final status written to S3 |
| Pod restart | In-memory history lost; S3 markdown preserved; chat marks session as ended |

After pod restart, the incident detail page shows the full investigation text from S3 but the chat input is disabled with a "Session ended — pod restarted" message.

---

## Out of Scope

- Write/mutate operations on the cluster (agent is strictly read-only)
- Multi-user chat (single conversation thread per incident)
- Incident assignment or acknowledgement workflows
- Alerting on investigation failures (investigation `failed` status is surfaced in the UI only)
- Retention / TTL policy for S3 incident files (handle via S3 lifecycle rules externally)
