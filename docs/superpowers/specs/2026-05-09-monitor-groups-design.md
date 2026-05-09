# Monitor Groups Design

**Date:** 2026-05-09

## Overview

Add a `group` field to monitor config so the dashboard can display monitors organized into named groups. The dashboard replaces its current flat FIRING/OK list with a grid of interactive group tiles.

## Config Schema

Add a single `group` string field to `MonitorConfig`:

```yaml
monitors:
  - name: api-gateway
    type: http
    group: global-infra   # new optional field
    ...
  - name: postgres
    type: kubernetes
    group: global-infra
    ...
  - name: yp-frontend
    type: http
    group: yellowpages
    ...
```

- `group` is optional. Monitors without a group render in an implicit "ungrouped" tile.
- Groups are ordered by first appearance in config.
- No separate `groups:` top-level block — names and order are derived from the monitors themselves.

## Backend Changes

### `internal/config/types.go`
Add `Group string \`yaml:"group"\`` to `MonitorConfig`.

### `internal/state/state.go`
Add `Group string \`json:"group"\`` to `AlarmState`.

### `internal/engine/engine.go`
Seed `Group` from `MonitorConfig` into `AlarmState` alongside `Icon` and `Priority`.

No API changes needed — `AlarmState` is already serialized wholesale by `/api/status`.

## Frontend Changes

### `ui/src/api/client.ts`
- Add `group?: string` to `AlarmState` interface.
- Add `group?: string` to `MonitorConfig` interface.

### `ui/src/pages/Dashboard.tsx`
Replace the flat FIRING/OK rendered lists with:

1. **Group tile grid** — one tile per group, ordered by first appearance in config. "ungrouped" appended last if any ungrouped monitors exist.
2. **Per-group tile** — contains one app-icon tile per monitor within the group. Each app tile shows:
   - Type icon (globe=http, hexagon=kubernetes, flame=prometheus/prometheus_scrape)
   - Colored by status: green=OK, red=FIRING, yellow=UNKNOWN
   - Monitor name below the icon (truncated)
3. **Drill-down panel** — clicking a group tile toggles a panel that slides open below the grid, showing the group's full `AlarmCard` list sorted firing-first.
4. **Card detail** — clicking an `AlarmCard` within the drill-down toggles its inline detail panel (existing behavior, unchanged).
5. Multiple groups can be open simultaneously.

### `ui/src/index.css`
Add glassmorphism styles:
- Group tiles: `backdrop-filter: blur`, translucent background, subtle border.
- Monitor app-icon tiles: 60×60px rounded squares, status-colored background + border, inner gradient highlight (`::before`).
- Hover: `translateY(-2px)` lift + status-colored glow via `box-shadow`.
- Drill-down panel: glass card with blue border.

### `ui/src/components/AlarmCard.tsx`
No changes — existing card behavior is reused as-is inside drill-down panels.

## Ungrouped Monitors

Monitors without a `group` field are shown in an implicit tile labeled "ungrouped" appended after all named groups. This ensures no monitor is silently hidden.

## Out of Scope

- No group-level icons (group name only).
- No group ordering config (order derived from monitor list).
- No filter chips by group (existing type/status chips remain).
- The 24h Timeline block and summary count cards remain unchanged.
