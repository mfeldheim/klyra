package state

import (
	"strings"
	"time"
)

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
	MonitorName   string      `json:"monitorName"`
	Status        AlarmStatus `json:"status"`
	LastCheck     time.Time   `json:"lastCheck"`
	FiredAt       *time.Time  `json:"firedAt,omitempty"`
	PendingSince  *time.Time  `json:"pendingSince,omitempty"`
	RecoverySince *time.Time  `json:"recoverySince,omitempty"`
	LastValue     any         `json:"lastValue,omitempty"`
	Message       string      `json:"message,omitempty"`
	Icon          string      `json:"icon,omitempty"`
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
	Icon        string
	Priority    *int
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

func (ps *PersistedState) Trim(window time.Duration, now time.Time) {
	cutoff := now.Add(-window)
	filtered := ps.History[:0]
	for _, e := range ps.History {
		if e.At.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	ps.History = filtered
}

// iconMap maps friendly icon names to emoji characters.
var iconMap = map[string]string{
	"globe":      "🌐",
	"kubernetes": "☸️",
	"k8s":        "☸️",
	"pipe":       "🔌",
	"pipeline":   "🔌",
	"tunnel":     "🚇",
	"cloud":      "☁️",
	"server":     "🖥️",
	"database":   "🗄️",
	"db":         "🗄️",
	"network":    "📡",
	"alert":      "🚨",
	"warning":    "⚠️",
	"fire":       "🔥",
	"cpu":        "🧮",
	"memory":     "💾",
	"disk":       "💽",
	"check":      "✅",
	"clock":      "🕐",
	"lock":       "🔒",
}

// ResolveIcon maps a named icon or raw emoji to its display form.
// Unknown names are passed through as-is (allows raw emoji in config).
func ResolveIcon(name string) string {
	if name == "" {
		return ""
	}
	if emoji, ok := iconMap[strings.ToLower(name)]; ok {
		return emoji
	}
	return name
}

// ParsePriority converts a priority string to a Pushover priority int.
// Returns nil when the string is empty (no override — use action default).
// Levels: low=-1, normal=0, high=1, critical=2 (emergency, repeats until ack).
func ParsePriority(s string) *int {
	var v int
	switch strings.ToLower(s) {
	case "low":
		v = -1
	case "normal":
		v = 0
	case "high":
		v = 1
	case "critical":
		v = 2
	default:
		return nil
	}
	return &v
}
