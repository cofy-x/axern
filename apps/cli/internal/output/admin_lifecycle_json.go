package output

import (
	"io"

	privateadminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/admin/v1"
)

type AllocationLifecycleRetryResponseJSON struct {
	Retry *AllocationLifecycleRetryJSON `json:"retry"`
}

type AllocationLifecycleRetryListJSON struct {
	Retries []*AllocationLifecycleRetryJSON `json:"retries"`
}

type AllocationLifecycleRetryJSON struct {
	AllocationID       string `json:"allocation_id"`
	RunID              string `json:"run_id"`
	EnvironmentID      string `json:"environment_id,omitempty"`
	LifecycleState     string `json:"lifecycle_state"`
	NodeID             string `json:"node_id"`
	NodeTarget         string `json:"node_target,omitempty"`
	ReconcileAttempts  int32  `json:"reconcile_attempts"`
	LastError          string `json:"last_error,omitempty"`
	NextRunAt          string `json:"next_run_at,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
	AgeSeconds         int64  `json:"age_seconds"`
	Due                bool   `json:"due"`
	Clearable          bool   `json:"clearable"`
	ClearBlockedReason string `json:"clear_blocked_reason,omitempty"`
}

func PrintAllocationLifecycleRetryJSON(w io.Writer, retry *privateadminv1.AllocationLifecycleRetry) error {
	return PrintJSON(w, AllocationLifecycleRetryResponseJSON{Retry: NewAllocationLifecycleRetryJSON(retry)})
}

func PrintAllocationLifecycleRetryListJSON(w io.Writer, retries []*privateadminv1.AllocationLifecycleRetry) error {
	out := AllocationLifecycleRetryListJSON{Retries: make([]*AllocationLifecycleRetryJSON, 0, len(retries))}
	for _, retry := range retries {
		out.Retries = append(out.Retries, NewAllocationLifecycleRetryJSON(retry))
	}
	return PrintJSON(w, out)
}

func NewAllocationLifecycleRetryJSON(retry *privateadminv1.AllocationLifecycleRetry) *AllocationLifecycleRetryJSON {
	if retry == nil {
		return nil
	}
	return &AllocationLifecycleRetryJSON{
		AllocationID:       retry.GetAllocationID(),
		RunID:              retry.GetRunID(),
		EnvironmentID:      retry.GetEnvironmentID(),
		LifecycleState:     retry.GetLifecycleState().String(),
		NodeID:             retry.GetNodeID(),
		NodeTarget:         retry.GetNodeTarget(),
		ReconcileAttempts:  retry.GetReconcileAttempts(),
		LastError:          retry.GetLastError(),
		NextRunAt:          FormatProtoTimestamp(retry.GetNextRunAt()),
		CreatedAt:          FormatProtoTimestamp(retry.GetCreatedAt()),
		UpdatedAt:          FormatProtoTimestamp(retry.GetUpdatedAt()),
		AgeSeconds:         retry.GetAgeSeconds(),
		Due:                retry.GetDue(),
		Clearable:          retry.GetClearable(),
		ClearBlockedReason: retry.GetClearBlockedReason(),
	}
}
