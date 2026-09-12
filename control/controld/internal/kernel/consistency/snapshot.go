package consistencykernel

type Status string

type Severity string

type IssueCode string

const (
	StatusOK           Status = "ok"
	StatusInconsistent Status = "inconsistent"

	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

const (
	IssueActiveReservationOnEndedAllocation IssueCode = "active_reservation_on_ended_allocation"
	IssueActiveLeaseOnEndedAllocation       IssueCode = "active_lease_on_ended_allocation"
	IssueActiveTunnelOnEndedAllocation      IssueCode = "active_tunnel_on_ended_allocation"
)

type Snapshot struct {
	Status    Status  `json:"status"`
	Counts    Counts  `json:"counts"`
	Issues    []Issue `json:"issues"`
	Truncated bool    `json:"truncated"`
}

type Counts struct {
	ActiveReservations int64 `json:"active_reservations"`
	ActiveLeases       int64 `json:"active_leases"`
	ActiveTunnels      int64 `json:"active_tunnels"`
	ReconcileQueue     int64 `json:"reconcile_queue"`
	Issues             int64 `json:"issues"`
}

type Issue struct {
	Code         IssueCode `json:"code"`
	Severity     Severity  `json:"severity"`
	AllocationID string    `json:"allocation_id,omitempty"`
	RunID        string    `json:"run_id,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	DependentID  string    `json:"dependent_id,omitempty"`
	Status       string    `json:"status,omitempty"`
	Detail       string    `json:"detail,omitempty"`
}

func NewSnapshot(counts Counts, issues []Issue, truncated bool) Snapshot {
	if issues == nil {
		issues = []Issue{}
	}
	status := StatusOK
	if len(issues) > 0 {
		status = StatusInconsistent
	}
	counts.Issues = int64(len(issues))
	return Snapshot{
		Status:    status,
		Counts:    counts,
		Issues:    issues,
		Truncated: truncated,
	}
}
