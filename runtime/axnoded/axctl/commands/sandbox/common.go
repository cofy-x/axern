package sandbox

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	nodeoperatorv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/operator/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func renderSandboxTable(w io.Writer, allocations []*nodeoperatorv1.LocalAllocation) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "ALLOCATION ID\tSTATE\tEXIT CODE\tPID\tSTARTED AT\tFINISHED AT")
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			allocation.GetAllocationID(),
			localStateString(allocation.GetState()),
			localExitCodeString(allocation.GetState(), allocation.ExitCode),
			formatPID(allocation.GetPid()),
			formatTimestamp(allocation.GetStartedAt()),
			formatTimestamp(allocation.GetFinishedAt()),
		)
	}
	_ = tw.Flush()
}

func renderSandboxInspect(w io.Writer, allocation *nodeoperatorv1.LocalAllocation) {
	if allocation == nil {
		return
	}
	fmt.Fprintf(w, "Allocation: %s\n", allocation.GetAllocationID())
	fmt.Fprintf(w, "State: %s\n", localStateString(allocation.GetState()))
	fmt.Fprintf(w, "Exit Code: %s\n", localExitCodeString(allocation.GetState(), allocation.ExitCode))
	fmt.Fprintf(w, "PID: %s\n", formatPID(allocation.GetPid()))
	if message := strings.TrimSpace(allocation.GetMessage()); message != "" {
		fmt.Fprintf(w, "Message: %s\n", message)
	}
	fmt.Fprintf(w, "Started At: %s\n", formatTimestamp(allocation.GetStartedAt()))
	fmt.Fprintf(w, "Finished At: %s\n", formatTimestamp(allocation.GetFinishedAt()))
}

func localStateString(state nodeoperatorv1.LocalAllocationState) string {
	switch state {
	case nodeoperatorv1.LocalAllocationState_LOCAL_ALLOCATION_STATE_RUNNING:
		return "RUNNING"
	case nodeoperatorv1.LocalAllocationState_LOCAL_ALLOCATION_STATE_EXITED:
		return "EXITED"
	default:
		return "UNKNOWN"
	}
}

func localExitCodeString(state nodeoperatorv1.LocalAllocationState, exitCode *int32) string {
	if state == nodeoperatorv1.LocalAllocationState_LOCAL_ALLOCATION_STATE_RUNNING {
		return "-"
	}
	if exitCode == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *exitCode)
}

func formatTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func formatPID(pid int32) string {
	if pid <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", pid)
}
