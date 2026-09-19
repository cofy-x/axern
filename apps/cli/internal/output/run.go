package output

import (
	"fmt"
	"io"
	"time"

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
	if digest := run.GetResolvedEnvironmentSpec().GetImageDescriptor().GetDigest(); digest != "" {
		fmt.Fprintf(w, "Environment Digest: %s\n", digest)
	}
	fmt.Fprintf(w, "Status: %s\n", RunStatusLabel(run.GetStatus()))
	if run.GetAllocationID() != "" {
		fmt.Fprintf(w, "Allocation ID: %s\n", run.GetAllocationID())
	}
	if expiry := run.GetOutputExpiresAt(); expiry != nil {
		fmt.Fprintf(w, "Output Expires At: %s\n", FormatProtoTimestamp(expiry))
	}
	if snapshot := run.GetRootfsSnapshot(); snapshot != nil {
		fmt.Fprintf(w, "Rootfs Snapshot: %s\n", snapshot.GetStatus().String())
		if snapshot.GetEnvironmentID() != "" {
			fmt.Fprintf(w, "Snapshot Environment ID: %s\n", snapshot.GetEnvironmentID())
		}
		if snapshot.GetMessage() != "" {
			fmt.Fprintf(w, "Snapshot Message: %s\n", snapshot.GetMessage())
		}
	}
	if len(run.GetConfig().GetArgv()) > 0 {
		fmt.Fprintf(w, "Argv: %v\n", run.GetConfig().GetArgv())
	}
	if mounts := run.GetConfig().GetImageMounts(); len(mounts) > 0 {
		fmt.Fprintf(w, "Image Mounts: %s\n", formatImageMounts(mounts))
	}
	if run.ExitCode != nil {
		fmt.Fprintf(w, "Exit Code: %d\n", run.GetExitCode())
	}
	if message := run.GetMessage(); message != "" {
		if code := run.GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			fmt.Fprintf(w, "Diagnostic: %s\n", WorkloadDiagnosticCodeLabel(code))
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
			firstNonEmpty(run.GetAllocationID(), "-"),
			formatRunAge(run),
		})
	}
	RenderTable(w, []string{"ID", "NAMESPACE", "STATUS", "ALLOCATION", "AGE"}, rows)
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
