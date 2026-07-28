package sandbox

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func renderSandboxTable(w io.Writer, sandboxes []*nodeoperatorv1.LocalSandbox) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "SANDBOX ID\tRUNTIME\tSTATE\tEXIT CODE\tPID\tSTARTED AT\tFINISHED AT")
	for _, sandbox := range sandboxes {
		if sandbox == nil {
			continue
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			sandbox.GetSandboxID(),
			sandbox.GetRuntimeClass(),
			localStateString(sandbox.GetState()),
			localExitCodeString(sandbox.GetState(), sandbox.GetExitCode(), sandbox.GetExitCodeKnown()),
			formatPID(sandbox.GetPid()),
			formatTimestamp(sandbox.GetStartedAt()),
			formatTimestamp(sandbox.GetFinishedAt()),
		)
	}
	_ = tw.Flush()
}

func renderSandboxInspect(w io.Writer, sandbox *nodeoperatorv1.LocalSandbox) {
	if sandbox == nil {
		return
	}
	fmt.Fprintf(w, "Sandbox: %s\n", sandbox.GetSandboxID())
	fmt.Fprintf(w, "Runtime: %s\n", sandbox.GetRuntimeClass())
	fmt.Fprintf(w, "State: %s\n", localStateString(sandbox.GetState()))
	fmt.Fprintf(w, "Exit Code: %s\n", localExitCodeString(sandbox.GetState(), sandbox.GetExitCode(), sandbox.GetExitCodeKnown()))
	fmt.Fprintf(w, "PID: %s\n", formatPID(sandbox.GetPid()))
	if message := strings.TrimSpace(sandbox.GetMessage()); message != "" {
		fmt.Fprintf(w, "Message: %s\n", message)
	}
	fmt.Fprintf(w, "Started At: %s\n", formatTimestamp(sandbox.GetStartedAt()))
	fmt.Fprintf(w, "Finished At: %s\n", formatTimestamp(sandbox.GetFinishedAt()))
}

func localStateString(state nodeoperatorv1.LocalSandboxState) string {
	switch state {
	case nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_RUNNING:
		return "RUNNING"
	case nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED:
		return "EXITED"
	default:
		return "UNKNOWN"
	}
}

func localExitCodeString(state nodeoperatorv1.LocalSandboxState, exitCode int32, known bool) string {
	if state == nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_RUNNING {
		return "-"
	}
	if !known {
		return "unknown"
	}
	return fmt.Sprintf("%d", exitCode)
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
