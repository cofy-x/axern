package retention

import "time"

const (
	DefaultEnabled          = true
	DefaultInterval         = 10 * time.Minute
	DefaultBatchSize        = 500
	DefaultTunnelEventsTTL  = 7 * 24 * time.Hour
	DefaultTunnelEventsKeep = 100
	DefaultQuotaEventsTTL   = 7 * 24 * time.Hour
	DefaultTerminalRunsTTL  = 7 * 24 * time.Hour
	DefaultAccessGrantsTTL  = 24 * time.Hour
)

type Config struct {
	Enabled          bool
	Interval         time.Duration
	BatchSize        int
	TunnelEventsTTL  time.Duration
	TunnelEventsKeep int
	QuotaEventsTTL   time.Duration
	TerminalRunsTTL  time.Duration
	AccessGrantsTTL  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Enabled:          DefaultEnabled,
		Interval:         DefaultInterval,
		BatchSize:        DefaultBatchSize,
		TunnelEventsTTL:  DefaultTunnelEventsTTL,
		TunnelEventsKeep: DefaultTunnelEventsKeep,
		QuotaEventsTTL:   DefaultQuotaEventsTTL,
		TerminalRunsTTL:  DefaultTerminalRunsTTL,
		AccessGrantsTTL:  DefaultAccessGrantsTTL,
	}
}

func NormalizeConfig(cfg Config) Config {
	out := cfg
	if out.Interval <= 0 {
		out.Interval = DefaultInterval
	}
	if out.BatchSize <= 0 {
		out.BatchSize = DefaultBatchSize
	}
	if out.TunnelEventsTTL <= 0 {
		out.TunnelEventsTTL = DefaultTunnelEventsTTL
	}
	if out.TunnelEventsKeep < 0 {
		out.TunnelEventsKeep = DefaultTunnelEventsKeep
	}
	if out.QuotaEventsTTL <= 0 {
		out.QuotaEventsTTL = DefaultQuotaEventsTTL
	}
	if out.TerminalRunsTTL <= 0 {
		out.TerminalRunsTTL = DefaultTerminalRunsTTL
	}
	if out.AccessGrantsTTL <= 0 {
		out.AccessGrantsTTL = DefaultAccessGrantsTTL
	}
	return out
}
