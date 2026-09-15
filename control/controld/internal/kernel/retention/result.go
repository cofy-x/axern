package retention

import "time"

type Result struct {
	TunnelEventsDeleted       int64
	QuotaEventsDeleted        int64
	TerminalRunsDeleted       int64
	AccessGrantsDeleted       int64
	EnrollmentReceiptsDeleted int64
	Skipped                   bool
	Duration                  time.Duration
}

func (r Result) TotalDeleted() int64 {
	return r.TunnelEventsDeleted + r.QuotaEventsDeleted + r.TerminalRunsDeleted + r.AccessGrantsDeleted + r.EnrollmentReceiptsDeleted
}
