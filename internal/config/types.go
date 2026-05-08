package config

import "time"

type Config struct {
	Monitors []MonitorConfig `yaml:"monitors"`
	Actions  []ActionConfig  `yaml:"actions"`
}

type MonitorConfig struct {
	Name      string          `yaml:"name"`
	Type      string          `yaml:"type"`
	Interval  Duration        `yaml:"interval"`
	Config    map[string]any  `yaml:"config"`
	Threshold ThresholdConfig `yaml:"threshold"`
	Actions   []string        `yaml:"actions"`
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
