package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
)

func retentionConfigFromEnv() retentionkernel.Config {
	cfg := retentionkernel.DefaultConfig()
	cfg.Enabled = boolFromEnv("CONTROLD_RETENTION_ENABLED", cfg.Enabled)
	cfg.Interval = durationFromEnv("CONTROLD_RETENTION_INTERVAL", cfg.Interval)
	cfg.BatchSize = intFromEnv("CONTROLD_RETENTION_BATCH_SIZE", cfg.BatchSize)
	cfg.TunnelEventsTTL = durationFromEnv("CONTROLD_RETENTION_TUNNEL_EVENTS_TTL", cfg.TunnelEventsTTL)
	cfg.TunnelEventsKeep = intFromEnv("CONTROLD_RETENTION_TUNNEL_EVENTS_KEEP", cfg.TunnelEventsKeep)
	cfg.QuotaEventsTTL = durationFromEnv("CONTROLD_RETENTION_QUOTA_EVENTS_TTL", cfg.QuotaEventsTTL)
	cfg.TerminalRunsTTL = durationFromEnv("CONTROLD_RETENTION_TERMINAL_RUNS_TTL", cfg.TerminalRunsTTL)
	cfg.AccessGrantsTTL = durationFromEnv("CONTROLD_RETENTION_ACCESS_GRANTS_TTL", cfg.AccessGrantsTTL)
	return retentionkernel.NormalizeConfig(cfg)
}

func boolFromEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intFromEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
