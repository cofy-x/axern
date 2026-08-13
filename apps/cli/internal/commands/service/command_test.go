package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestProbeBuildLeavesDefaultDurationsUnset(t *testing.T) {
	probe, err := (probeOptions{httpPort: 8080, path: "/ready"}).build()
	if err != nil {
		t.Fatal(err)
	}
	if probe.GetInitialDelay() != nil || probe.GetPeriod() != nil || probe.GetTimeout() != nil {
		t.Fatalf("default durations must remain unset: %+v", probe)
	}
}

func TestDeleteCommandExposesBoundedWait(t *testing.T) {
	cmd := deleteCommand(command.Runtime{})
	if cmd.Flags().Lookup("wait") == nil || cmd.Flags().Lookup("wait-timeout") == nil {
		t.Fatal("service delete is missing --wait or --wait-timeout")
	}
	waitTimeout, err := cmd.Flags().GetDuration("wait-timeout")
	if err != nil {
		t.Fatal(err)
	}
	if waitTimeout != appservice.DefaultDeleteWaitTimeout {
		t.Fatalf("--wait-timeout default = %s, want %s", waitTimeout, appservice.DefaultDeleteWaitTimeout)
	}
}

func TestDeleteCommandRejectsTimeoutWithoutWait(t *testing.T) {
	cmd := deleteCommand(command.Runtime{})
	if err := cmd.Flags().Set("wait-timeout", "1s"); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, []string{"svc-1"})
	var usageErr command.UsageError
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "--wait-timeout requires --wait") {
		t.Fatalf("delete command error = %v, want usage error", err)
	}
}

func TestDeleteCommandRejectsNegativeWaitTimeout(t *testing.T) {
	cmd := deleteCommand(command.Runtime{})
	if err := cmd.Flags().Set("wait", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("wait-timeout", "-1s"); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, []string{"svc-1"})
	var usageErr command.UsageError
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "--wait-timeout must not be negative") {
		t.Fatalf("delete command error = %v, want negative timeout usage error", err)
	}
}

func TestListCommandDocumentsDeletedAuditView(t *testing.T) {
	cmd := listCommand(command.Runtime{})
	if !strings.Contains(cmd.Long, "--status deleted") || !strings.Contains(cmd.Long, "excludes terminal deleted records") {
		t.Fatalf("service list long help = %q, want deleted audit guidance", cmd.Long)
	}
}

func TestProbeBuildPreservesExplicitDurations(t *testing.T) {
	probe, err := (probeOptions{tcpPort: 8080, initialDelay: time.Second, period: 500 * time.Millisecond, timeout: 200 * time.Millisecond}).build()
	if err != nil {
		t.Fatal(err)
	}
	if got := probe.GetPeriod().AsDuration(); got != 500*time.Millisecond {
		t.Fatalf("period = %s", got)
	}
}

func TestProbeBuildRejectsInvalidValues(t *testing.T) {
	for name, options := range map[string]probeOptions{
		"port":      {httpPort: 65536},
		"duration":  {httpPort: 8080, period: -time.Second},
		"threshold": {httpPort: 8080, failureThreshold: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := options.build(); err == nil {
				t.Fatal("build() error = nil")
			}
		})
	}
}

func TestUpdateCommandExposesCompleteExecutionConfig(t *testing.T) {
	cmd := updateCommand(command.Runtime{})
	for _, name := range serviceExecutionFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("service update is missing --%s", name)
		}
	}
	value := `print("a,b")`
	if err := cmd.Flags().Set("argv", value); err != nil {
		t.Fatal(err)
	}
	got, err := cmd.Flags().GetStringArray("argv")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != value {
		t.Fatalf("argv = %#v", got)
	}
}

func TestExecutionUpdatePreservesUnspecifiedFields(t *testing.T) {
	o := &createOptions{}
	cmd := updateCommand(command.Runtime{})
	if err := cmd.Flags().Set("request-cpu", "750m"); err != nil {
		t.Fatal(err)
	}
	o.requestCPU = "750m"
	current := &commonv1.ExecutionConfig{
		Argv:         []string{"sleep", "300"},
		Env:          map[string]string{"MODE": "test"},
		RuntimeClass: "runsc",
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{CpuMilli: 250, MemoryBytes: 128},
			Limits:   &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 512},
		},
	}

	got, err := o.executionUpdate(cmd, current)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetResources().GetRequests().GetCpuMilli() != 750 || got.GetResources().GetRequests().GetMemoryBytes() != 128 {
		t.Fatalf("requests = %+v, want cpu updated and memory preserved", got.GetResources().GetRequests())
	}
	if !proto.Equal(got.GetResources().GetLimits(), current.GetResources().GetLimits()) {
		t.Fatalf("limits = %+v, want %+v", got.GetResources().GetLimits(), current.GetResources().GetLimits())
	}
	if len(got.GetArgv()) != 2 || got.GetArgv()[0] != "sleep" || got.GetRuntimeClass() != "runsc" || got.GetEnv()["MODE"] != "test" {
		t.Fatalf("execution fields were not preserved: %+v", got)
	}
	if current.GetResources().GetRequests().GetCpuMilli() != 250 {
		t.Fatalf("current config was mutated: %+v", current.GetResources().GetRequests())
	}
}

func TestExecutionUpdateReplacesOnlyExplicitList(t *testing.T) {
	o := &createOptions{argv: []string{"echo", "updated"}}
	cmd := updateCommand(command.Runtime{})
	if err := cmd.Flags().Set("argv", "echo"); err != nil {
		t.Fatal(err)
	}
	current := &commonv1.ExecutionConfig{Argv: []string{"sleep", "300"}, Env: map[string]string{"MODE": "test"}}

	got, err := o.executionUpdate(cmd, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetArgv()) != 2 || got.GetArgv()[1] != "updated" {
		t.Fatalf("argv = %#v", got.GetArgv())
	}
	if got.GetEnv()["MODE"] != "test" {
		t.Fatalf("env = %#v, want preserved", got.GetEnv())
	}
}
