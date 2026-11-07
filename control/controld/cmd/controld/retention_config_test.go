package main

import (
	"os"
	"testing"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
)

func TestParseFlagsRetentionPolicyEnvironmentAndOverrides(t *testing.T) {
	values := map[string]string{
		"CONTROLD_RETENTION_ENABLED":            "false",
		"CONTROLD_RETENTION_INTERVAL":           "3m",
		"CONTROLD_RETENTION_BATCH_SIZE":         "23",
		"CONTROLD_RETENTION_TUNNEL_EVENTS_TTL":  "11h",
		"CONTROLD_RETENTION_TUNNEL_EVENTS_KEEP": "7",
		"CONTROLD_RETENTION_QUOTA_EVENTS_TTL":   "12h",
		"CONTROLD_RETENTION_TERMINAL_RUNS_TTL":  "13h",
		"CONTROLD_RETENTION_ACCESS_GRANTS_TTL":  "14h",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	for _, tc := range []struct {
		name string
		args []string
		want retentionkernel.Config
	}{
		{
			name: "environment",
			want: retentionkernel.Config{Enabled: false, Interval: 3 * time.Minute, BatchSize: 23, TunnelEventsTTL: 11 * time.Hour, TunnelEventsKeep: 7, QuotaEventsTTL: 12 * time.Hour, TerminalRunsTTL: 13 * time.Hour, AccessGrantsTTL: 14 * time.Hour},
		},
		{
			name: "flags override environment",
			args: []string{"-retention-enabled=true", "-retention-interval=5m", "-retention-batch-size=17", "-retention-tunnel-events-ttl=21h", "-retention-tunnel-events-keep=0", "-retention-quota-events-ttl=22h", "-retention-terminal-runs-ttl=23h", "-retention-access-grants-ttl=24h"},
			want: retentionkernel.Config{Enabled: true, Interval: 5 * time.Minute, BatchSize: 17, TunnelEventsTTL: 21 * time.Hour, TunnelEventsKeep: 0, QuotaEventsTTL: 22 * time.Hour, TerminalRunsTTL: 23 * time.Hour, AccessGrantsTTL: 24 * time.Hour},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldArgs := os.Args
			t.Cleanup(func() { os.Args = oldArgs })
			os.Args = append([]string{
				"controld",
				"-postgres-dsn=postgres://test",
				"-secrets-master-key=test-only-master-key-32-bytes!!!",
				"-tls-ca-cert=ca.crt",
				"-workload-cluster=cluster.test",
				"-workload-bundle=controld.pem",
				"-workload-signer-bundle=signer.pem",
			}, tc.args...)
			opts, err := parseFlags()
			if err != nil {
				t.Fatal(err)
			}
			if opts.retention != tc.want {
				t.Fatalf("retention policy = %+v, want %+v", opts.retention, tc.want)
			}
		})
	}
}

func TestRetentionEnvironmentNormalizesInvalidPolicyValues(t *testing.T) {
	for _, name := range []string{
		"CONTROLD_RETENTION_ENABLED", "CONTROLD_RETENTION_INTERVAL", "CONTROLD_RETENTION_BATCH_SIZE", "CONTROLD_RETENTION_TUNNEL_EVENTS_TTL", "CONTROLD_RETENTION_TUNNEL_EVENTS_KEEP", "CONTROLD_RETENTION_QUOTA_EVENTS_TTL", "CONTROLD_RETENTION_TERMINAL_RUNS_TTL", "CONTROLD_RETENTION_ACCESS_GRANTS_TTL",
	} {
		t.Setenv(name, "invalid")
	}
	t.Setenv("CONTROLD_RETENTION_INTERVAL", "-1s")
	t.Setenv("CONTROLD_RETENTION_BATCH_SIZE", "-1")
	if got, want := retentionConfigFromEnv(), retentionkernel.DefaultConfig(); got != want {
		t.Fatalf("retention policy = %+v, want defaults %+v", got, want)
	}
}
