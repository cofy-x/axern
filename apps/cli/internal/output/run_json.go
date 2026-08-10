package output

import (
	"io"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

type RunListJSON struct {
	Runs       []*RunJSON `json:"runs"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type RunResponseJSON struct {
	Run *RunJSON `json:"run"`
}

type RunJSON struct {
	ID                   string                      `json:"id"`
	Namespace            string                      `json:"namespace"`
	EnvironmentID        string                      `json:"environment_id"`
	AllocationID         string                      `json:"allocation_id,omitempty"`
	Attempt              int64                       `json:"attempt,omitempty"`
	Status               string                      `json:"status"`
	Config               *ExecutionConfigJSON        `json:"config,omitempty"`
	Labels               map[string]string           `json:"labels,omitempty"`
	Version              int64                       `json:"version"`
	CreatedAt            string                      `json:"created_at,omitempty"`
	UpdatedAt            string                      `json:"updated_at,omitempty"`
	ExitCode             *int32                      `json:"exit_code,omitempty"`
	ExitCodeKnown        bool                        `json:"exit_code_known,omitempty"`
	DiagnosticCode       string                      `json:"diagnostic_code,omitempty"`
	AdmissionSummary     string                      `json:"admission_summary,omitempty"`
	Message              string                      `json:"message,omitempty"`
	CapabilityConditions *CapabilityConditionSetJSON `json:"capability_conditions,omitempty"`
}

func PrintRunListJSON(w io.Writer, resp *runv1.ListRunsResponse) error {
	out := RunListJSON{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Runs = make([]*RunJSON, 0, len(resp.GetRuns()))
		for _, run := range resp.GetRuns() {
			out.Runs = append(out.Runs, NewRunJSON(run))
		}
	}
	return PrintJSON(w, out)
}

func PrintRunResponseJSON(w io.Writer, run *runv1.Run) error {
	return PrintJSON(w, RunResponseJSON{Run: NewRunJSON(run)})
}

func NewRunJSON(run *runv1.Run) *RunJSON {
	if run == nil {
		return nil
	}
	exitCode := knownExitCode(run.GetExitCode(), run.GetExitCodeKnown())
	diagnosticCode := ""
	if code := run.GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		diagnosticCode = WorkloadDiagnosticCodeLabel(code)
	} else {
		diagnosticCode = workloaddiagnostic.DiagnosticCode(run.GetMessage())
	}
	return &RunJSON{
		ID:                   run.GetID(),
		Namespace:            run.GetNamespace(),
		EnvironmentID:        run.GetEnvironmentID(),
		AllocationID:         run.GetAllocationID(),
		Attempt:              run.GetAttempt(),
		Status:               RunStatusLabel(run.GetStatus()),
		Config:               NewExecutionConfigJSON(run.GetConfig()),
		Labels:               cloneStringMap(run.GetLabels()),
		Version:              run.GetVersion(),
		CreatedAt:            FormatProtoTimestamp(run.GetCreatedAt()),
		UpdatedAt:            FormatProtoTimestamp(run.GetUpdatedAt()),
		ExitCode:             exitCode,
		ExitCodeKnown:        run.GetExitCodeKnown(),
		DiagnosticCode:       diagnosticCode,
		AdmissionSummary:     workloaddiagnostic.AdmissionBlockedSummary(run.GetMessage()),
		Message:              run.GetMessage(),
		CapabilityConditions: newCapabilityConditionSetJSON(run.GetCapabilityConditions()),
	}
}

func knownExitCode(exitCode int32, known bool) *int32 {
	if !known {
		return nil
	}
	return &exitCode
}
