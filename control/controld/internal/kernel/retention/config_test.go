package retention

import (
	"testing"
	"time"
)

func TestDefaultConfigCoversRunAndTunnelHistory(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled || cfg.Interval <= 0 || cfg.BatchSize <= 0 {
		t.Fatalf("invalid default retention config: %+v", cfg)
	}
	if cfg.TunnelEventsTTL <= 0 || cfg.TunnelEventsKeep < 0 || cfg.QuotaEventsTTL <= 0 || cfg.TerminalRunsTTL <= 0 || cfg.LeasesTTL <= 0 {
		t.Fatalf("incomplete default retention config: %+v", cfg)
	}
}

func TestNormalizeConfigRestoresInvalidDurations(t *testing.T) {
	cfg := NormalizeConfig(Config{Enabled: true, Interval: -time.Second, BatchSize: -1})
	if cfg.Interval != DefaultInterval || cfg.BatchSize != DefaultBatchSize || cfg.TunnelEventsTTL != DefaultTunnelEventsTTL || cfg.TerminalRunsTTL != DefaultTerminalRunsTTL {
		t.Fatalf("normalized config = %+v", cfg)
	}
}

func TestResultTotalDeleted(t *testing.T) {
	result := Result{TunnelEventsDeleted: 1, QuotaEventsDeleted: 2, TerminalRunsDeleted: 3, LeasesDeleted: 4}
	if got := result.TotalDeleted(); got != 10 {
		t.Fatalf("TotalDeleted() = %d, want 10", got)
	}
}
