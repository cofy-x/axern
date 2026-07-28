package retention

import "time"

const (
	DefaultEnabled                 = true
	DefaultInterval                = 10 * time.Minute
	DefaultBatchSize               = 500
	DefaultServiceEventsTTL        = 7 * 24 * time.Hour
	DefaultServiceEventsKeep       = 100
	DefaultTunnelEventsTTL         = 7 * 24 * time.Hour
	DefaultTunnelEventsKeep        = 100
	DefaultQuotaEventsTTL          = 7 * 24 * time.Hour
	DefaultServiceReplicasTTL      = 24 * time.Hour
	DefaultServiceReplicasKeep     = 20
	DefaultTerminalRunsTTL         = 7 * 24 * time.Hour
	DefaultLeasesTTL               = 24 * time.Hour
	DefaultFunctionEventsTTL       = 7 * 24 * time.Hour
	DefaultFunctionEventsKeep      = 200
	DefaultFunctionInvocationsTTL  = 7 * 24 * time.Hour
	DefaultFunctionInvocationsKeep = 500
	DefaultFunctionIdempotencyTTL  = 24 * time.Hour
)

type Config struct {
	Enabled                 bool
	Interval                time.Duration
	BatchSize               int
	ServiceEventsTTL        time.Duration
	ServiceEventsKeep       int
	TunnelEventsTTL         time.Duration
	TunnelEventsKeep        int
	QuotaEventsTTL          time.Duration
	ServiceReplicasTTL      time.Duration
	ServiceReplicasKeep     int
	TerminalRunsTTL         time.Duration
	LeasesTTL               time.Duration
	FunctionEventsTTL       time.Duration
	FunctionEventsKeep      int
	FunctionInvocationsTTL  time.Duration
	FunctionInvocationsKeep int
	FunctionIdempotencyTTL  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Enabled:                 DefaultEnabled,
		Interval:                DefaultInterval,
		BatchSize:               DefaultBatchSize,
		ServiceEventsTTL:        DefaultServiceEventsTTL,
		ServiceEventsKeep:       DefaultServiceEventsKeep,
		TunnelEventsTTL:         DefaultTunnelEventsTTL,
		TunnelEventsKeep:        DefaultTunnelEventsKeep,
		QuotaEventsTTL:          DefaultQuotaEventsTTL,
		ServiceReplicasTTL:      DefaultServiceReplicasTTL,
		ServiceReplicasKeep:     DefaultServiceReplicasKeep,
		TerminalRunsTTL:         DefaultTerminalRunsTTL,
		LeasesTTL:               DefaultLeasesTTL,
		FunctionEventsTTL:       DefaultFunctionEventsTTL,
		FunctionEventsKeep:      DefaultFunctionEventsKeep,
		FunctionInvocationsTTL:  DefaultFunctionInvocationsTTL,
		FunctionInvocationsKeep: DefaultFunctionInvocationsKeep,
		FunctionIdempotencyTTL:  DefaultFunctionIdempotencyTTL,
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
	if out.ServiceEventsTTL <= 0 {
		out.ServiceEventsTTL = DefaultServiceEventsTTL
	}
	if out.ServiceEventsKeep < 0 {
		out.ServiceEventsKeep = DefaultServiceEventsKeep
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
	if out.ServiceReplicasTTL <= 0 {
		out.ServiceReplicasTTL = DefaultServiceReplicasTTL
	}
	if out.ServiceReplicasKeep < 0 {
		out.ServiceReplicasKeep = DefaultServiceReplicasKeep
	}
	if out.TerminalRunsTTL <= 0 {
		out.TerminalRunsTTL = DefaultTerminalRunsTTL
	}
	if out.LeasesTTL <= 0 {
		out.LeasesTTL = DefaultLeasesTTL
	}
	if out.FunctionEventsTTL <= 0 {
		out.FunctionEventsTTL = DefaultFunctionEventsTTL
	}
	if out.FunctionEventsKeep < 0 {
		out.FunctionEventsKeep = DefaultFunctionEventsKeep
	}
	if out.FunctionInvocationsTTL <= 0 {
		out.FunctionInvocationsTTL = DefaultFunctionInvocationsTTL
	}
	if out.FunctionInvocationsKeep < 0 {
		out.FunctionInvocationsKeep = DefaultFunctionInvocationsKeep
	}
	if out.FunctionIdempotencyTTL <= 0 {
		out.FunctionIdempotencyTTL = DefaultFunctionIdempotencyTTL
	}
	return out
}
