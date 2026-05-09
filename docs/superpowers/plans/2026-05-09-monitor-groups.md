# Monitor Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `group` field to monitor config and replace the flat dashboard list with a glassmorphism group-tile grid where each tile shows per-monitor app-icon status chips; clicking a group drills into its monitor cards.

**Architecture:** Backend adds `Group` to `MonitorConfig` and `AlarmState` and seeds it at engine startup. Frontend reads `group` from both `/api/status` and `/api/config`, builds an ordered group list from config order, and renders group tiles + collapsible drill-down panels instead of the current flat FIRING/OK lists.

**Tech Stack:** Go 1.21, React 18 + TypeScript, Vite, hand-rolled CSS (no UI library).

---

## File Map

| File | Change |
|------|--------|
| `internal/config/types.go` | Add `Group string` to `MonitorConfig` |
| `internal/state/state.go` | Add `Group string` to `AlarmState` |
| `internal/engine/engine.go` | Seed `Group` in alarm-seeding loop |
| `internal/config/loader_test.go` | Add test: `group` field round-trips through YAML parse |
| `internal/server/handlers_test.go` | Add test: `group` appears in `/api/status` response |
| `ui/src/api/client.ts` | Add `group?: string` to `AlarmState` and `MonitorConfig` |
| `ui/src/components/GroupTile.tsx` | New: group tile with per-monitor app-icon chips |
| `ui/src/pages/Dashboard.tsx` | Replace flat list with group grid + drill-down panels |
| `ui/src/index.css` | Add glassmorphism group/tile/drill-down styles |

---

## Task 1: Add `Group` to config, state, and engine

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/state/state.go`
- Modify: `internal/engine/engine.go`
- Modify: `internal/config/loader_test.go`
- Modify: `internal/server/handlers_test.go`

- [ ] **Step 1: Write failing test — config `group` field parses from YAML**

Add to `internal/config/loader_test.go`:

```go
func TestLoadConfigGroup(t *testing.T) {
	yaml := `
monitors:
  - name: api-gw
    type: http
    group: global-infra
    interval: 30s
    config:
      url: https://example.com
    threshold:
      operator: eq
      value: false
actions: []
`
	cfg, err := config.Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Monitors[0].Group != "global-infra" {
		t.Errorf("expected group 'global-infra', got %q", cfg.Monitors[0].Group)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
go test ./internal/config/... -run TestLoadConfigGroup -v
```

Expected: FAIL — `cfg.Monitors[0].Group` is empty string (field doesn't exist yet).

- [ ] **Step 3: Add `Group` to `MonitorConfig` in `internal/config/types.go`**

After the `Priority` field, add:

```go
Group    string          `yaml:"group"     json:"group,omitempty"`
```

Full `MonitorConfig` struct after change:

```go
type MonitorConfig struct {
	Name      string          `yaml:"name"      json:"name"`
	Type      string          `yaml:"type"      json:"type"`
	Interval  Duration        `yaml:"interval"  json:"interval"`
	Config    map[string]any  `yaml:"config"    json:"config,omitempty"`
	Threshold ThresholdConfig `yaml:"threshold" json:"threshold"`
	Actions   []string        `yaml:"actions"   json:"actions"`
	Icon      string          `yaml:"icon"      json:"icon,omitempty"`
	Priority  string          `yaml:"priority"  json:"priority,omitempty"`
	Group     string          `yaml:"group"     json:"group,omitempty"`
}
```

- [ ] **Step 4: Run config test — verify it passes**

```bash
go test ./internal/config/... -run TestLoadConfigGroup -v
```

Expected: PASS

- [ ] **Step 5: Write failing test — `group` appears in `/api/status` response**

Add to `internal/server/handlers_test.go`:

```go
func TestStatusHandlerGroup(t *testing.T) {
	st := state.NewStore()
	st.SetAlarm(state.AlarmState{
		MonitorName: "api-gw",
		Status:      state.AlarmOK,
		LastCheck:   time.Now(),
		Group:       "global-infra",
	})
	cfg := &config.Config{
		Monitors: []config.MonitorConfig{{Name: "api-gw", Type: "http", Group: "global-infra"}},
	}
	h := server.NewHandlers(st, cfg)
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	alarms := resp["alarms"].(map[string]any)
	alarm := alarms["api-gw"].(map[string]any)
	if alarm["group"] != "global-infra" {
		t.Errorf("expected group 'global-infra' in status response, got %v", alarm["group"])
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

```bash
go test ./internal/server/... -run TestStatusHandlerGroup -v
```

Expected: FAIL — `AlarmState` has no `Group` field yet.

- [ ] **Step 7: Add `Group` to `AlarmState` in `internal/state/state.go`**

After the `Icon` field:

```go
type AlarmState struct {
	MonitorName   string      `json:"monitorName"`
	Status        AlarmStatus `json:"status"`
	LastCheck     time.Time   `json:"lastCheck"`
	FiredAt       *time.Time  `json:"firedAt,omitempty"`
	PendingSince  *time.Time  `json:"pendingSince,omitempty"`
	RecoverySince *time.Time  `json:"recoverySince,omitempty"`
	LastValue     any         `json:"lastValue,omitempty"`
	Message       string      `json:"message,omitempty"`
	Icon          string      `json:"icon,omitempty"`
	Group         string      `json:"group,omitempty"`
}
```

- [ ] **Step 8: Seed `Group` in engine startup in `internal/engine/engine.go`**

Find the existing seeding loop (around line 110):

```go
// Seed icons into alarm states so the API serves them before the first check.
for _, mc := range e.cfg.Monitors {
    existing, _ := e.store.GetAlarm(mc.Name)
    existing.MonitorName = mc.Name
    existing.Icon = meta[mc.Name].icon
    e.store.SetAlarm(existing)
}
```

Replace with:

```go
// Seed monitor metadata into alarm states so the API serves them before the first check.
for _, mc := range e.cfg.Monitors {
    existing, _ := e.store.GetAlarm(mc.Name)
    existing.MonitorName = mc.Name
    existing.Icon = meta[mc.Name].icon
    existing.Group = mc.Group
    e.store.SetAlarm(existing)
}
```

- [ ] **Step 9: Run all Go tests**

```bash
go test ./... -v 2>&1 | tail -30
```

Expected: all PASS, no compilation errors.

- [ ] **Step 10: Commit**

```bash
git add internal/config/types.go internal/state/state.go internal/engine/engine.go \
        internal/config/loader_test.go internal/server/handlers_test.go
git commit -m "feat: add group field to MonitorConfig and AlarmState, seed at startup"
```

---

## Task 2: Update TypeScript interfaces

**Files:**
- Modify: `ui/src/api/client.ts`

- [ ] **Step 1: Add `group` to `AlarmState` and `MonitorConfig` interfaces**

In `ui/src/api/client.ts`, update the two interfaces:

```ts
export interface AlarmState {
  monitorName: string
  status: AlarmStatus
  lastCheck: string
  firedAt?: string
  lastValue?: unknown
  message?: string
  icon?: string
  group?: string
}
```

```ts
export interface MonitorConfig {
  name: string
  type: string
  interval: string
  threshold: { operator: string; value: unknown; for?: string }
  actions: string[]
  group?: string
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra/ui
npm run build 2>&1 | tail -20
```

Expected: build succeeds (zero type errors).

- [ ] **Step 3: Commit**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
git add ui/src/api/client.ts
git commit -m "feat: add group field to AlarmState and MonitorConfig TS interfaces"
```

---

## Task 3: GroupTile component

**Files:**
- Create: `ui/src/components/GroupTile.tsx`

- [ ] **Step 1: Create `ui/src/components/GroupTile.tsx`**

```tsx
import type { AlarmState } from '../api/client'

interface GroupTileProps {
  name: string
  alarms: AlarmState[]
  typeMap: Record<string, string>
  active: boolean
  onClick: () => void
}

function typeIcon(type: string): JSX.Element {
  switch (type) {
    case 'http':
      return (
        <svg viewBox="0 0 16 16" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="8" cy="8" r="7" />
          <path d="M8 1c-1.5 2-2.5 4.5-2.5 7s1 5 2.5 7M8 1c1.5 2 2.5 4.5 2.5 7s-1 5-2.5 7M1 8h14" />
        </svg>
      )
    case 'kubernetes':
      return (
        <svg viewBox="0 0 16 16" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5">
          <path d="M8 1L14.5 4.5v7L8 15 1.5 11.5v-7z" />
          <circle cx="8" cy="8" r="1.5" fill="currentColor" stroke="none" />
        </svg>
      )
    case 'prometheus':
    case 'prometheus_scrape':
      return (
        <svg viewBox="0 0 16 16" width="20" height="20" fill="currentColor">
          <path d="M8 2c0 2.5-3 3.5-3 7a3 3 0 0 0 6 0c0-1.5-1-2.5-1-4 0 0-1 1-1 2.5C8.5 9.5 7 8.5 7 7 7 5.5 8 2 8 2z" />
        </svg>
      )
    default:
      return (
        <svg viewBox="0 0 16 16" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="8" cy="8" r="7" />
          <text x="8" y="12" textAnchor="middle" fontSize="9">?</text>
        </svg>
      )
  }
}

export function GroupTile({ name, alarms, typeMap, active, onClick }: GroupTileProps) {
  const hasFiring = alarms.some(a => a.status === 'FIRING')

  return (
    <div
      className={`group-tile${hasFiring ? ' has-firing' : ''}${active ? ' active' : ''}`}
      onClick={onClick}
    >
      <div className="group-tile-name">{name}</div>
      <div className="monitor-tiles">
        {alarms.map(a => (
          <div
            key={a.monitorName}
            className={`monitor-tile ${a.status.toLowerCase()}`}
            title={a.monitorName}
          >
            {typeIcon(typeMap[a.monitorName] ?? '')}
            <div className="monitor-tile-name">{a.monitorName}</div>
          </div>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra/ui
npm run build 2>&1 | tail -20
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
git add ui/src/components/GroupTile.tsx
git commit -m "feat: GroupTile component with per-monitor app-icon status tiles"
```

---

## Task 4: Glassmorphism CSS

**Files:**
- Modify: `ui/src/index.css`

- [ ] **Step 1: Add glassmorphism styles to `ui/src/index.css`**

Append to the end of the file:

```css
/* ── Group tiles ──────────────────────────────────── */
body {
  background-image:
    radial-gradient(ellipse at 20% 30%, #1a2a4a33 0%, transparent 60%),
    radial-gradient(ellipse at 80% 70%, #2a1a3a33 0%, transparent 60%);
}

.group-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 14px; margin-bottom: 20px; }

.group-tile {
  background: rgba(255,255,255,0.04);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(255,255,255,0.09);
  border-radius: 14px;
  padding: 16px;
  cursor: pointer;
  user-select: none;
  transition: border-color 0.2s, background 0.2s, box-shadow 0.2s;
  box-shadow: 0 4px 24px rgba(0,0,0,0.3), inset 0 1px 0 rgba(255,255,255,0.06);
}
.group-tile:hover {
  border-color: rgba(74,144,217,0.5);
  background: rgba(74,144,217,0.06);
  box-shadow: 0 6px 32px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.08);
}
.group-tile.has-firing { border-color: rgba(244,67,54,0.3); background: rgba(244,67,54,0.04); }
.group-tile.has-firing:hover { border-color: rgba(244,67,54,0.6); background: rgba(244,67,54,0.07); }
.group-tile.active {
  border-color: rgba(74,144,217,0.6) !important;
  background: rgba(74,144,217,0.08) !important;
  box-shadow: 0 6px 32px rgba(74,144,217,0.15), inset 0 1px 0 rgba(255,255,255,0.1);
}

.group-tile-name {
  font-size: 11px; text-transform: uppercase; letter-spacing: 0.09em;
  color: rgba(255,255,255,0.35); margin-bottom: 14px; font-weight: 500;
}

/* ── Monitor app-icon tiles inside group ──────────── */
.monitor-tiles { display: flex; flex-wrap: wrap; gap: 8px; }

.monitor-tile {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  width: 60px; height: 60px; border-radius: 14px; gap: 5px; padding: 6px 4px;
  transition: transform 0.15s, box-shadow 0.15s;
  position: relative; overflow: hidden;
}
.monitor-tile::before {
  content: ''; position: absolute; inset: 0; border-radius: 14px;
  background: linear-gradient(135deg, rgba(255,255,255,0.12) 0%, rgba(255,255,255,0) 60%);
  pointer-events: none;
}
.monitor-tile:hover { transform: translateY(-2px); }

.monitor-tile.ok {
  background: rgba(76,175,80,0.15); border: 1px solid rgba(76,175,80,0.3); color: #6fcf7a;
  box-shadow: 0 2px 12px rgba(76,175,80,0.15), inset 0 1px 0 rgba(255,255,255,0.08);
}
.monitor-tile.firing {
  background: rgba(244,67,54,0.15); border: 1px solid rgba(244,67,54,0.35); color: #f47570;
  box-shadow: 0 2px 12px rgba(244,67,54,0.2), inset 0 1px 0 rgba(255,255,255,0.08);
}
.monitor-tile.unknown {
  background: rgba(255,152,0,0.15); border: 1px solid rgba(255,152,0,0.35); color: #ffb74d;
  box-shadow: 0 2px 12px rgba(255,152,0,0.15), inset 0 1px 0 rgba(255,255,255,0.08);
}
.monitor-tile.ok:hover    { box-shadow: 0 4px 20px rgba(76,175,80,0.45),  inset 0 1px 0 rgba(255,255,255,0.12); border-color: rgba(76,175,80,0.6); }
.monitor-tile.firing:hover { box-shadow: 0 4px 20px rgba(244,67,54,0.45), inset 0 1px 0 rgba(255,255,255,0.12); border-color: rgba(244,67,54,0.65); }
.monitor-tile.unknown:hover{ box-shadow: 0 4px 20px rgba(255,152,0,0.4),  inset 0 1px 0 rgba(255,255,255,0.12); border-color: rgba(255,152,0,0.6); }

.monitor-tile-name {
  font-size: 9px; font-weight: 500; opacity: 0.85;
  max-width: 54px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: center;
}

/* ── Drill-down panel ─────────────────────────────── */
.drill-panel {
  background: rgba(255,255,255,0.04);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(74,144,217,0.4);
  border-radius: 14px;
  margin-bottom: 20px;
  overflow: hidden;
  box-shadow: 0 8px 32px rgba(0,0,0,0.4), 0 0 0 1px rgba(74,144,217,0.1);
  animation: drillSlideDown 0.18s ease;
}
@keyframes drillSlideDown {
  from { opacity: 0; transform: translateY(-6px); }
  to   { opacity: 1; transform: translateY(0); }
}
.drill-header {
  display: flex; align-items: center; padding: 12px 16px;
  background: rgba(74,144,217,0.08);
  border-bottom: 1px solid rgba(255,255,255,0.06);
}
.drill-header-name { font-weight: 700; font-size: 14px; color: #e6edf3; }
.drill-header-close {
  margin-left: auto; color: rgba(255,255,255,0.3); cursor: pointer;
  font-size: 20px; line-height: 1; background: none; border: none;
}
.drill-header-close:hover { color: rgba(255,255,255,0.7); }
.drill-body { padding: 12px 14px; display: flex; flex-direction: column; gap: 6px; }
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra/ui
npm run build 2>&1 | tail -10
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
git add ui/src/index.css
git commit -m "feat: glassmorphism CSS for group tiles, monitor app-icon tiles, drill-down panel"
```

---

## Task 5: Refactor Dashboard to group tiles + drill-down

**Files:**
- Modify: `ui/src/pages/Dashboard.tsx`

- [ ] **Step 1: Replace `Dashboard.tsx` with group-tile implementation**

```tsx
import { useEffect, useState } from 'react'
import { api, type AlarmState, type HistoryEvent, type ConfigResponse } from '../api/client'
import { AlarmCard } from '../components/AlarmCard'
import { GroupTile } from '../components/GroupTile'
import { Timeline } from '../components/Timeline'

export function Dashboard() {
  const [alarms, setAlarms] = useState<Record<string, AlarmState>>({})
  const [history, setHistory] = useState<HistoryEvent[]>([])
  const [cfg, setCfg] = useState<ConfigResponse | null>(null)
  const [openGroups, setOpenGroups] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)

  useEffect(() => {
    const load = () => {
      api.status().then(r => setAlarms(r.alarms)).catch(() => {})
      api.history().then(setHistory).catch(() => {})
      api.config().then(setCfg).catch(() => {})
    }
    load()
    const id = setInterval(load, 30000)
    return () => clearInterval(id)
  }, [])

  const typeMap: Record<string, string> = {}
  cfg?.monitors?.forEach(m => { typeMap[m.name] = m.type })

  const all = Object.values(alarms)
  const firing = all.filter(a => a.status === 'FIRING')
  const ok = all.filter(a => a.status === 'OK')
  const unknown = all.filter(a => a.status === 'UNKNOWN')

  // Build ordered group list from config order, ungrouped last.
  const groupOrder: string[] = []
  cfg?.monitors?.forEach(m => {
    const g = m.group ?? ''
    if (!groupOrder.includes(g)) groupOrder.push(g)
  })
  // Also catch any groups that appear in alarms but not config (edge case).
  all.forEach(a => {
    const g = a.group ?? ''
    if (!groupOrder.includes(g)) groupOrder.push(g)
  })
  // Move '' (ungrouped) to the end.
  const ungroupedIdx = groupOrder.indexOf('')
  if (ungroupedIdx !== -1) {
    groupOrder.splice(ungroupedIdx, 1)
    groupOrder.push('')
  }

  // Bucket alarms by group, firing-first within each group.
  const grouped: Record<string, AlarmState[]> = {}
  all.forEach(a => {
    const g = a.group ?? ''
    if (!grouped[g]) grouped[g] = []
    grouped[g].push(a)
  })
  const statusOrder = { FIRING: 0, UNKNOWN: 1, OK: 2 } as const
  Object.values(grouped).forEach(arr =>
    arr.sort((a, b) => (statusOrder[a.status] ?? 3) - (statusOrder[b.status] ?? 3))
  )

  function toggleGroup(name: string) {
    setOpenGroups(prev => {
      const next = new Set(prev)
      next.has(name) ? next.delete(name) : next.add(name)
      return next
    })
  }

  const visibleGroups = groupOrder.filter(g => grouped[g]?.length)

  return (
    <div className="main">
      {/* Summary cards */}
      <div className="summary">
        <div className={`summary-card${firing.length ? ' firing' : ''}`}>
          <div className={`summary-num ${firing.length ? 'red' : 'gray'}`}>{firing.length}</div>
          <div className="summary-label">Firing</div>
        </div>
        <div className="summary-card">
          <div className="summary-num green">{ok.length}</div>
          <div className="summary-label">OK</div>
        </div>
        <div className="summary-card">
          <div className="summary-num gray">{unknown.length}</div>
          <div className="summary-label">Unknown</div>
        </div>
        <div className="summary-card">
          <div className="summary-num gray">{all.length}</div>
          <div className="summary-label">Total</div>
        </div>
      </div>

      {/* 24h timeline */}
      {all.length > 0 && (
        <div className="block" style={{ marginBottom: 20 }}>
          <h3>24h overview</h3>
          {all.map(a => (
            <Timeline key={a.monitorName} monitorName={a.monitorName} events={history} currentStatus={a.status} />
          ))}
        </div>
      )}

      {/* Group tile grid */}
      {visibleGroups.length > 0 && (
        <div className="group-grid">
          {visibleGroups.map(g => (
            <GroupTile
              key={g || '__ungrouped__'}
              name={g || 'ungrouped'}
              alarms={grouped[g]}
              typeMap={typeMap}
              active={openGroups.has(g)}
              onClick={() => toggleGroup(g)}
            />
          ))}
        </div>
      )}

      {/* Drill-down panels — one per open group, in order */}
      {visibleGroups.filter(g => openGroups.has(g)).map(g => (
        <div key={g || '__ungrouped__'} className="drill-panel">
          <div className="drill-header">
            <div className="drill-header-name">{g || 'ungrouped'}</div>
            <button className="drill-header-close" onClick={() => toggleGroup(g)}>×</button>
          </div>
          <div className="drill-body">
            {grouped[g].map(a => (
              <AlarmCard
                key={a.monitorName}
                alarm={a}
                monitorType={typeMap[a.monitorName]}
                selected={selected === a.monitorName}
                onSelect={a2 => setSelected(prev => prev === a2.monitorName ? null : a2.monitorName)}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 2: Build and type-check**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra/ui
npm run build 2>&1 | tail -20
```

Expected: zero errors.

- [ ] **Step 3: Start dev server and verify in browser**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra/ui
npm run dev
```

Open `http://localhost:5173`. Verify:
- Summary cards render at top
- Group tile grid renders below timeline (or empty state if no monitors have a `group` field — all land in "ungrouped")
- Clicking a group tile opens the drill-down panel with `AlarmCard` rows
- Clicking again closes the panel
- Clicking a monitor card inside the panel slides open its detail
- Monitor tiles inside group tiles are colored by status and have type icons

- [ ] **Step 4: Run all Go tests to confirm backend unchanged**

```bash
cd /Users/michelfeldheim/Documents/Workspaces/infra/klyra
go test ./... 2>&1 | tail -15
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/Dashboard.tsx ui/src/components/GroupTile.tsx
git commit -m "feat: group tile dashboard — glassmorphism grid with per-monitor status tiles and drill-down"
```

---

## Self-Review

**Spec coverage:**
- ✅ `group` field on `MonitorConfig` (Task 1)
- ✅ `group` seeded into `AlarmState` at engine startup (Task 1)
- ✅ `group` in `/api/status` JSON response (Task 1)
- ✅ TypeScript interfaces updated (Task 2)
- ✅ GroupTile component with per-monitor app-icon tiles (Task 3)
- ✅ Glassmorphism CSS: group tiles, monitor tiles, hover glow, drill-down (Task 4)
- ✅ Dashboard group grid, ordered by config, firing-first within group (Task 5)
- ✅ Drill-down panel with AlarmCard rows (Task 5)
- ✅ Ungrouped monitors in "ungrouped" tile (Task 5, `g || 'ungrouped'`)
- ✅ Multiple groups can be open simultaneously (Task 5, `openGroups` Set)

**Placeholder scan:** None found.

**Type consistency:**
- `GroupTile` props `name: string`, `alarms: AlarmState[]`, `typeMap: Record<string, string>`, `active: boolean`, `onClick: () => void` — all used consistently in Dashboard.
- `monitor-tile` CSS class uses `a.status.toLowerCase()` which maps `OK`→`ok`, `FIRING`→`firing`, `UNKNOWN`→`unknown` — all three have CSS rules.
- `drill-panel`, `drill-header`, `drill-header-name`, `drill-header-close`, `drill-body` CSS classes all defined in Task 4 and used in Task 5.
