package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/diagnostics"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
)

// Deps holds the dependencies used by the klyra-native MCP tools.
type Deps struct {
	K8s       *diagnostics.K8sTools
	Store     *state.Store
	Config    *config.Config
	Incidents incident.Store // may be nil when incidents are not configured
}

// BuildTools assembles the full MCP tool set: read-only Kubernetes diagnostics
// plus klyra-native tools (status, monitors, silences, incidents, history).
func BuildTools(d Deps) []Tool {
	tools := k8sTools(d.K8s)
	tools = append(tools, klyraTools(d)...)
	return tools
}

func k8sTools(kt *diagnostics.K8sTools) []Tool {
	if kt == nil {
		return nil
	}
	defs := diagnostics.Definitions()
	out := make([]Tool, 0, len(defs))
	for _, def := range defs {
		def := def
		out = append(out, Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
			Call: func(ctx context.Context, args json.RawMessage) (string, bool) {
				res := kt.Execute(ctx, def.Name, args)
				return res.Content, res.IsError
			},
		})
	}
	return out
}

func jsonText(v any) (string, bool) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err.Error(), true
	}
	return string(b), false
}

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

type monitorView struct {
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Interval  string                 `json:"interval"`
	Group     string                 `json:"group,omitempty"`
	Priority  string                 `json:"priority,omitempty"`
	Threshold config.ThresholdConfig `json:"threshold"`
	Actions   []string               `json:"actions,omitempty"`
}

func klyraTools(d Deps) []Tool {
	var tools []Tool

	if d.Store != nil {
		tools = append(tools,
			Tool{
				Name:        "klyra_status",
				Description: "Get current klyra alarm states for all monitors (status, firedAt, lastValue, message, group).",
				InputSchema: emptySchema(),
				Call: func(_ context.Context, _ json.RawMessage) (string, bool) {
					return jsonText(map[string]any{"alarms": d.Store.Alarms()})
				},
			},
			Tool{
				Name:        "klyra_history",
				Description: "Get recent klyra alarm transition history (FIRING/RESOLVED events across all monitors).",
				InputSchema: emptySchema(),
				Call: func(_ context.Context, _ json.RawMessage) (string, bool) {
					return jsonText(d.Store.History())
				},
			},
			Tool{
				Name:        "klyra_list_silences",
				Description: "List active and scheduled klyra monitor silences.",
				InputSchema: emptySchema(),
				Call: func(_ context.Context, _ json.RawMessage) (string, bool) {
					return jsonText(d.Store.Silences())
				},
			},
		)
	}

	if d.Config != nil {
		tools = append(tools, Tool{
			Name:        "klyra_list_monitors",
			Description: "List configured klyra monitors with their type, interval, group, threshold and actions.",
			InputSchema: emptySchema(),
			Call: func(_ context.Context, _ json.RawMessage) (string, bool) {
				views := make([]monitorView, 0, len(d.Config.Monitors))
				for _, m := range d.Config.Monitors {
					views = append(views, monitorView{
						Name:      m.Name,
						Type:      m.Type,
						Interval:  m.Interval.String(),
						Group:     m.Group,
						Priority:  m.Priority,
						Threshold: m.Threshold,
						Actions:   m.Actions,
					})
				}
				return jsonText(views)
			},
		})
	}

	if d.Incidents != nil {
		tools = append(tools,
			Tool{
				Name:        "klyra_list_incidents",
				Description: "List recent klyra incidents (newest first) with monitor, fired/resolved time and status.",
				InputSchema: emptySchema(),
				Call: func(ctx context.Context, _ json.RawMessage) (string, bool) {
					idx, err := d.Incidents.ListIncidents(ctx)
					if err != nil {
						return "failed to list incidents: " + err.Error(), true
					}
					incs := idx.Incidents
					sort.SliceStable(incs, func(i, j int) bool {
						return incs[i].FiredAt.After(incs[j].FiredAt)
					})
					const limit = 50
					if len(incs) > limit {
						incs = incs[:limit]
					}
					return jsonText(map[string]any{"incidents": incs})
				},
			},
			Tool{
				Name:        "klyra_get_incident",
				Description: "Get a single klyra incident by ID, including its metadata and recorded markdown log.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "string", "description": "Incident ID (e.g. inc-...-...)"},
					},
					"required": []string{"id"},
				},
				Call: func(ctx context.Context, args json.RawMessage) (string, bool) {
					var p struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
						return "missing required parameter: id", true
					}
					inc, err := d.Incidents.ReadIncident(ctx, p.ID)
					if err != nil {
						return fmt.Sprintf("incident %q not found: %v", p.ID, err), true
					}
					content, cerr := d.Incidents.ReadContent(ctx, p.ID)
					if cerr != nil {
						content = ""
					}
					return jsonText(map[string]any{"incident": inc, "content": content})
				},
			},
		)
	}

	return tools
}
