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
