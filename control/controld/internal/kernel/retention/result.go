package retention

import "time"

type Result struct {
	ServiceEventsDeleted       int64
	TunnelEventsDeleted        int64
	QuotaEventsDeleted         int64
	ServiceAllocationsDeleted  int64
	TerminalRunsDeleted        int64
	LeasesDeleted              int64
	FunctionEventsDeleted      int64
	FunctionInvocationsDeleted int64
	FunctionIdempotencyDeleted int64
	Skipped                    bool
	Duration                   time.Duration
}

func (r Result) TotalDeleted() int64 {
	return r.ServiceEventsDeleted + r.TunnelEventsDeleted + r.QuotaEventsDeleted +
		r.ServiceAllocationsDeleted + r.TerminalRunsDeleted + r.LeasesDeleted +
		r.FunctionEventsDeleted + r.FunctionInvocationsDeleted + r.FunctionIdempotencyDeleted
}
