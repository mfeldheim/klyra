package config_test

import (
	"strings"
	"testing"

	"github.com/mfeldheim/klyra/internal/config"
)

const testYAML = `
monitors:
  - name: test-http
    type: http
    interval: 30s
    config:
      url: https://example.com
      expect_status: 200
    threshold:
      operator: eq
      value: false
      for: 1m
    actions:
      - notify
actions:
  - name: notify
    type: http
    config:
      url: https://ntfy.sh/test
      auth:
        type: bearer
        token: ${TEST_TOKEN}
`

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret123")

	cfg, err := config.Load(strings.NewReader(testYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(cfg.Monitors))
	}
	if cfg.Monitors[0].Name != "test-http" {
		t.Errorf("unexpected monitor name: %s", cfg.Monitors[0].Name)
	}
	if cfg.Monitors[0].Interval.Seconds() != 30 {
		t.Errorf("unexpected interval: %v", cfg.Monitors[0].Interval)
	}
	actionCfg := cfg.Actions[0].Config
	authMap, _ := actionCfg["auth"].(map[string]any)
	if authMap["token"] != "secret123" {
		t.Errorf("expected token secret123, got %v", authMap["token"])
	}
}

func TestLoadConfigMissingEnvVar(t *testing.T) {
	_, err := config.Load(strings.NewReader(testYAML))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

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
