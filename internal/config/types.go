package config

import (
	"encoding/json"
	"time"
)

type Config struct {
	Monitors []MonitorConfig `yaml:"monitors" json:"monitors"`
	Actions  []ActionConfig  `yaml:"actions" json:"actions"`
}

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

type ThresholdConfig struct {
	Operator    string   `yaml:"operator"     json:"operator"`
	Value       any      `yaml:"value"        json:"value"`
	For         Duration `yaml:"for"          json:"for"`
	RecoveryFor Duration `yaml:"recovery_for" json:"recovery_for,omitempty"`
}

type ActionConfig struct {
	Name   string         `yaml:"name"   json:"name"`
	Type   string         `yaml:"type"   json:"type"`
	Config map[string]any `yaml:"config" json:"-"`
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

func (d Duration) MarshalJSON() ([]byte, error) {
	if d.Duration == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(d.Duration.String())
}
