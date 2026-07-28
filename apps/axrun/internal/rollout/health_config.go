package rollout

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// HealthCheckConfigFromEnv reads health check configuration from
// environment variables. Health check is enabled by default.
func HealthCheckConfigFromEnv() HealthCheckConfig {
	config := HealthCheckConfig{
		Enabled: true,
	}
	if v := strings.TrimSpace(os.Getenv("AXRUN_SANDBOX_HEALTH_ENABLED")); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			config.Enabled = enabled
		}
	}
	if v := strings.TrimSpace(os.Getenv("AXRUN_SANDBOX_HEALTH_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			config.Interval = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("AXRUN_SANDBOX_HEALTH_THRESHOLD")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			config.Threshold = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("AXRUN_SANDBOX_HEALTH_PROBE_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			config.ProbeTimeout = d
		}
	}
	return config
}
