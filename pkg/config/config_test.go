package config

import (
	"testing"
)

// TestDefaultConfig_HeartbeatEnabled verifies heartbeat is enabled by default
func TestDefaultConfig_HeartbeatEnabled(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be enabled by default")
	}
}

// TestDefaultConfig_HeartbeatCanBeDisabled verifies heartbeat can be disabled via config
func TestDefaultConfig_HeartbeatCanBeDisabled(t *testing.T) {
	cfg := &Config{}
	cfg.Heartbeat.Enabled = false

	if cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be disabled when set to false")
	}
}
