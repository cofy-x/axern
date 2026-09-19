package output

import (
	"io"

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
	ID                      string                       `json:"id"`
	Namespace               string                       `json:"namespace"`
	EnvironmentID           string                       `json:"environment_id"`
	EnvironmentSpec         *EnvironmentSpecJSON         `json:"environment_spec,omitempty"`
	ResolvedEnvironmentSpec *ResolvedEnvironmentSpecJSON `json:"resolved_environment_spec,omitempty"`
	AllocationID            string                       `json:"allocation_id,omitempty"`
	Status                  string                       `json:"status"`
	Config                  *ExecutionConfigJSON         `json:"config,omitempty"`
	Labels                  map[string]string            `json:"labels,omitempty"`
	Version                 int64                        `json:"version"`
	CreatedAt               string                       `json:"created_at,omitempty"`
	UpdatedAt               string                       `json:"updated_at,omitempty"`
	ExitCode                *int32                       `json:"exit_code,omitempty"`
	DiagnosticCode          string                       `json:"diagnostic_code,omitempty"`
	Message                 string                       `json:"message,omitempty"`
	CapabilityConditions    *CapabilityConditionSetJSON  `json:"capability_conditions,omitempty"`
	RootfsSnapshot          *RootfsSnapshotResultJSON    `json:"rootfs_snapshot,omitempty"`
}

type RootfsSnapshotResultJSON struct {
	Status          string `json:"status"`
	EnvironmentID   string `json:"environment_id,omitempty"`
	ImageRef        string `json:"image_ref,omitempty"`
	Digest          string `json:"digest,omitempty"`
	PlatformOS      string `json:"platform_os,omitempty"`
	PlatformArch    string `json:"platform_arch,omitempty"`
	PlatformVariant string `json:"platform_variant,omitempty"`
	Message         string `json:"message,omitempty"`
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
	diagnosticCode := ""
	if code := run.GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		diagnosticCode = WorkloadDiagnosticCodeLabel(code)
	}
	return &RunJSON{
		ID:                      run.GetID(),
		Namespace:               run.GetNamespace(),
		EnvironmentID:           run.GetEnvironmentID(),
		EnvironmentSpec:         newEnvironmentSpecJSON(run.GetEnvironmentSpec()),
		ResolvedEnvironmentSpec: newResolvedEnvironmentSpecJSON(run.GetResolvedEnvironmentSpec()),
		AllocationID:            run.GetAllocationID(),
		Status:                  RunStatusLabel(run.GetStatus()),
		Config:                  NewExecutionConfigJSON(run.GetConfig()),
		Labels:                  cloneStringMap(run.GetLabels()),
		Version:                 run.GetVersion(),
		CreatedAt:               FormatProtoTimestamp(run.GetCreatedAt()),
		UpdatedAt:               FormatProtoTimestamp(run.GetUpdatedAt()),
		ExitCode:                run.ExitCode,
		DiagnosticCode:          diagnosticCode,
		Message:                 run.GetMessage(),
		CapabilityConditions:    newCapabilityConditionSetJSON(run.GetCapabilityConditions()),
		RootfsSnapshot:          newRootfsSnapshotResultJSON(run.GetRootfsSnapshot()),
	}
}

func newRootfsSnapshotResultJSON(result *runv1.RootfsSnapshotResult) *RootfsSnapshotResultJSON {
	if result == nil {
		return nil
	}
	return &RootfsSnapshotResultJSON{Status: result.GetStatus().String(), EnvironmentID: result.GetEnvironmentID(), ImageRef: result.GetImageRef(), Digest: result.GetImageDescriptor().GetDigest(), PlatformOS: result.GetPlatformOS(), PlatformArch: result.GetPlatformArch(), PlatformVariant: result.GetPlatformVariant(), Message: result.GetMessage()}
}
