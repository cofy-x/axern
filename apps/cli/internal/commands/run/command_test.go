package run

import (
	"errors"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestRunIsForegroundRootAndCreateIsRemoved(t *testing.T) {
	cmd := Command(command.Runtime{})
	for _, name := range []string{"template", "environment", "file", "detach", "wait-timeout", "snapshot-rootfs"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("run flag --%s is missing", name)
		}
	}
	for _, removed := range []string{"argv", "image-ref", "template-id", "environment-id", "wait", "wait-for"} {
		if cmd.Flags().Lookup(removed) != nil {
			t.Fatalf("removed run flag --%s is still registered", removed)
		}
	}
	if found, _, err := cmd.Find([]string{"create"}); err == nil && found != cmd {
		t.Fatal("run create is still registered")
	}
}

func TestExecutionConfigRequestsRootfsSnapshotByPresence(t *testing.T) {
	config, err := executionConfig(createOptions{rootfsSnapshot: true})
	if err != nil {
		t.Fatalf("executionConfig() error = %v", err)
	}
	if config.GetRootfsSnapshot() == nil {
		t.Fatal("executionConfig() omitted the rootfs snapshot contract")
	}
}

func TestRunRejectsSourceSpecificFlags(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"template version without template", []string{"--environment", "env-1", "--template-version", "v2"}, "--template-version requires --template"},
		{"template registry credential", []string{"--template", "python311", "--registry-credential-id", "credential-1"}, "cannot be combined with --template"},
		{"environment readonly rootfs", []string{"--environment", "env-1", "--rootfs-readonly"}, "require an image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := Command(command.Runtime{})
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRunFileRejectsPositionalDefinition(t *testing.T) {
	cmd := Command(command.Runtime{})
	if err := cmd.Flags().Set("file", "run.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, []string{"python:3.12-slim"}); err == nil {
		t.Fatal("--file accepted a positional image")
	}
}

func TestCanReadOutputAfterInitialWait(t *testing.T) {
	timeout := errors.New("timed out waiting for run")
	for _, test := range []struct {
		name    string
		run     *runv1.Run
		waitErr error
		want    bool
	}{
		{name: "running", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_RUNNING}, want: true},
		{name: "succeeded before running observation", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED}, want: true},
		{name: "terminal failure output remains diagnostic", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_FAILED}, waitErr: errors.New("run failed"), want: true},
		{name: "placed timeout", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_PLACED}, waitErr: timeout, want: false},
		{name: "starting timeout", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_STARTING}, waitErr: timeout, want: false},
		{name: "missing observation", waitErr: timeout, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := canReadOutputAfterInitialWait(test.run, test.waitErr); got != test.want {
				t.Fatalf("canReadOutputAfterInitialWait() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestTerminalWorkloadExitError(t *testing.T) {
	exitCode := int32(7)
	for _, test := range []struct {
		name     string
		run      *runv1.Run
		wantCode int
	}{
		{name: "failed workload", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_FAILED, ExitCode: &exitCode}, wantCode: 7},
		{name: "failed infrastructure without exit", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_FAILED}},
		{name: "running observation does not expose exit", run: &runv1.Run{Status: runv1.RunStatus_RUN_STATUS_RUNNING, ExitCode: &exitCode}},
		{name: "nil run"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := terminalWorkloadExitError(test.run)
			if test.wantCode == 0 {
				if err != nil {
					t.Fatalf("terminalWorkloadExitError() error = %v", err)
				}
				return
			}
			var exitErr command.ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != test.wantCode {
				t.Fatalf("terminalWorkloadExitError() error = %#v, want exit code %d", err, test.wantCode)
			}
		})
	}
}
