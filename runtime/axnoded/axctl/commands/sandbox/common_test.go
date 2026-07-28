package sandbox

import (
	"bytes"
	"strings"
	"testing"
	"time"

	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderSandboxTableFormatsMissingValues(t *testing.T) {
	var out bytes.Buffer
	renderSandboxTable(&out, []*nodeoperatorv1.LocalSandbox{
		{
			SandboxID:     "demo",
			RuntimeClass:  "runsc",
			State:         nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_RUNNING,
			ExitCodeKnown: false,
			Pid:           0,
		},
	})

	got := out.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("renderSandboxTable() returned %d lines, want 2:\n%s", len(lines), got)
	}
	for _, want := range []string{"SANDBOX ID", "EXIT CODE", "FINISHED AT"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("renderSandboxTable() header missing %q:\n%s", want, got)
		}
	}
	fields := strings.Fields(lines[1])
	wantFields := []string{"demo", "runsc", "RUNNING", "-", "-", "-", "-"}
	if len(fields) != len(wantFields) {
		t.Fatalf("renderSandboxTable() fields = %v, want %v", fields, wantFields)
	}
	for i, want := range wantFields {
		if fields[i] != want {
			t.Fatalf("renderSandboxTable() field %d = %q, want %q (line: %q)", i, fields[i], want, lines[1])
		}
	}
}

func TestRenderSandboxInspectKeepsUnknownExitCodeForExitedSandbox(t *testing.T) {
	var out bytes.Buffer
	renderSandboxInspect(&out, &nodeoperatorv1.LocalSandbox{
		SandboxID:     "demo",
		RuntimeClass:  "runc",
		State:         nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED,
		ExitCodeKnown: false,
	})

	got := out.String()
	if !strings.Contains(got, "Exit Code: unknown") {
		t.Fatalf("renderSandboxInspect() output missing unknown exit code for exited sandbox:\n%s", got)
	}
}

func TestRenderSandboxInspectFormatsMissingValues(t *testing.T) {
	var out bytes.Buffer
	renderSandboxInspect(&out, &nodeoperatorv1.LocalSandbox{
		SandboxID:     "demo",
		RuntimeClass:  "runc",
		State:         nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED,
		ExitCodeKnown: true,
		ExitCode:      0,
	})

	got := out.String()
	for _, want := range []string{
		"Sandbox: demo",
		"PID: -",
		"Started At: -",
		"Finished At: -",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSandboxInspect() output missing %q:\n%s", want, got)
		}
	}
}

func TestFormatTimestampUsesUTC(t *testing.T) {
	ts := timestamppb.New(time.Date(2026, 4, 23, 10, 44, 16, 0, time.FixedZone("CST", 8*3600)))
	got := formatTimestamp(ts)
	if got != "2026-04-23T02:44:16Z" {
		t.Fatalf("formatTimestamp() = %q, want %q", got, "2026-04-23T02:44:16Z")
	}
}
