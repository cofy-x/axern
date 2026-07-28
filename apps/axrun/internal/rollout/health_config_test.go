package rollout

import (
	"testing"
	"time"
)

func TestHealthCheckConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "")
	t.Setenv("AXRUN_SANDBOX_HEALTH_INTERVAL", "")
	t.Setenv("AXRUN_SANDBOX_HEALTH_THRESHOLD", "")
	t.Setenv("AXRUN_SANDBOX_HEALTH_PROBE_TIMEOUT", "")

	config := HealthCheckConfigFromEnv()
	if !config.Enabled {
		t.Fatal("expected health check enabled by default")
	}
	if config.Interval != 0 || config.Threshold != 0 || config.ProbeTimeout != 0 {
		t.Fatalf("unexpected default overrides: %#v", config)
	}
}

func TestHealthCheckConfigFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "false")
	t.Setenv("AXRUN_SANDBOX_HEALTH_INTERVAL", "30s")
	t.Setenv("AXRUN_SANDBOX_HEALTH_THRESHOLD", "5")
	t.Setenv("AXRUN_SANDBOX_HEALTH_PROBE_TIMEOUT", "7s")

	config := HealthCheckConfigFromEnv()
	if config.Enabled {
		t.Fatal("expected health check to be disabled")
	}
	if config.Interval != 30*time.Second || config.Threshold != 5 || config.ProbeTimeout != 7*time.Second {
		t.Fatalf("config = %#v", config)
	}
}

func TestHealthCheckConfigFromEnvIgnoresInvalidValues(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "not-bool")
	t.Setenv("AXRUN_SANDBOX_HEALTH_INTERVAL", "-1s")
	t.Setenv("AXRUN_SANDBOX_HEALTH_THRESHOLD", "0")
	t.Setenv("AXRUN_SANDBOX_HEALTH_PROBE_TIMEOUT", "oops")

	config := HealthCheckConfigFromEnv()
	if !config.Enabled {
		t.Fatal("invalid bool should keep default enabled=true")
	}
	if config.Interval != 0 || config.Threshold != 0 || config.ProbeTimeout != 0 {
		t.Fatalf("config should ignore invalid numeric values: %#v", config)
	}
}
