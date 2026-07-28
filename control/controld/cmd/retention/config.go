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
	cfg.ServiceEventsTTL = durationFromEnv("CONTROLD_RETENTION_SERVICE_EVENTS_TTL", cfg.ServiceEventsTTL)
	cfg.ServiceEventsKeep = intFromEnv("CONTROLD_RETENTION_SERVICE_EVENTS_KEEP", cfg.ServiceEventsKeep)
	cfg.TunnelEventsTTL = durationFromEnv("CONTROLD_RETENTION_TUNNEL_EVENTS_TTL", cfg.TunnelEventsTTL)
	cfg.TunnelEventsKeep = intFromEnv("CONTROLD_RETENTION_TUNNEL_EVENTS_KEEP", cfg.TunnelEventsKeep)
	cfg.QuotaEventsTTL = durationFromEnv("CONTROLD_RETENTION_QUOTA_EVENTS_TTL", cfg.QuotaEventsTTL)
	cfg.ServiceReplicasTTL = durationFromEnv("CONTROLD_RETENTION_SERVICE_REPLICAS_TTL", cfg.ServiceReplicasTTL)
	cfg.ServiceReplicasKeep = intFromEnv("CONTROLD_RETENTION_SERVICE_REPLICAS_KEEP", cfg.ServiceReplicasKeep)
	cfg.TerminalRunsTTL = durationFromEnv("CONTROLD_RETENTION_TERMINAL_RUNS_TTL", cfg.TerminalRunsTTL)
	cfg.LeasesTTL = durationFromEnv("CONTROLD_RETENTION_LEASES_TTL", cfg.LeasesTTL)
	cfg.FunctionEventsTTL = durationFromEnv("CONTROLD_RETENTION_FUNCTION_EVENTS_TTL", cfg.FunctionEventsTTL)
	cfg.FunctionEventsKeep = intFromEnv("CONTROLD_RETENTION_FUNCTION_EVENTS_KEEP", cfg.FunctionEventsKeep)
	cfg.FunctionInvocationsTTL = durationFromEnv("CONTROLD_RETENTION_FUNCTION_INVOCATIONS_TTL", cfg.FunctionInvocationsTTL)
	cfg.FunctionInvocationsKeep = intFromEnv("CONTROLD_RETENTION_FUNCTION_INVOCATIONS_KEEP", cfg.FunctionInvocationsKeep)
	cfg.FunctionIdempotencyTTL = durationFromEnv("CONTROLD_RETENTION_FUNCTION_IDEMPOTENCY_TTL", cfg.FunctionIdempotencyTTL)
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
