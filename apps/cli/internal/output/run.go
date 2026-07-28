package output

import (
	"fmt"
	"io"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func RenderRun(w io.Writer, run *runv1.Run) {
	if run == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", run.GetID())
	fmt.Fprintf(w, "Namespace: %s\n", run.GetNamespace())
	fmt.Fprintf(w, "Environment ID: %s\n", run.GetEnvironmentID())
	fmt.Fprintf(w, "Status: %s\n", RunStatusLabel(run.GetStatus()))
	if run.GetAllocationID() != "" {
		fmt.Fprintf(w, "Allocation ID: %s\n", run.GetAllocationID())
	}
	fmt.Fprintf(w, "Attempt: %d\n", run.GetAttempt())
	if len(run.GetConfig().GetArgv()) > 0 {
		fmt.Fprintf(w, "Argv: %v\n", run.GetConfig().GetArgv())
	}
	if mounts := run.GetConfig().GetImageMounts(); len(mounts) > 0 {
		fmt.Fprintf(w, "Image Mounts: %s\n", formatImageMounts(mounts))
	}
	if run.GetExitCodeKnown() {
		fmt.Fprintf(w, "Exit Code: %d\n", run.GetExitCode())
	}
	if message := run.GetMessage(); message != "" {
		if code := run.GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			fmt.Fprintf(w, "Diagnostic: %s\n", WorkloadDiagnosticCodeLabel(code))
		} else if diagnostic := workloaddiagnostic.DiagnosticCode(message); diagnostic != "" {
			fmt.Fprintf(w, "Diagnostic: %s\n", diagnostic)
		}
		if admission := workloaddiagnostic.AdmissionBlockedSummary(message); admission != "" {
			fmt.Fprintf(w, "Admission: %s\n", admission)
		}
		fmt.Fprintf(w, "Message: %s\n", message)
	}
}

func RenderRunTable(w io.Writer, runs []*runv1.Run) {
	rows := make([][]string, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		rows = append(rows, []string{
			run.GetID(),
			firstNonEmpty(run.GetNamespace(), "-"),
			RunStatusLabel(run.GetStatus()),
			fmt.Sprintf("%d", run.GetAttempt()),
			firstNonEmpty(run.GetAllocationID(), "-"),
			formatRunAge(run),
		})
	}
	RenderTable(w, []string{"ID", "NAMESPACE", "STATUS", "ATTEMPT", "ALLOCATION", "AGE"}, rows)
}

func formatRunAge(run *runv1.Run) string {
	if run == nil {
		return "-"
	}
	if created := run.GetCreatedAt(); created != nil {
		return FormatRelativeAge(created.AsTime(), time.Now().UTC())
	}
	if updated := run.GetUpdatedAt(); updated != nil {
		return FormatRelativeAge(updated.AsTime(), time.Now().UTC())
	}
	return "-"
}
