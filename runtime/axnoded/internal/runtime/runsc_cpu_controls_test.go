package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

type cpuCreateExecutor struct {
	recordingExecutor
	createErr error
	stateErr  error
	statePID  int
}

func (r *cpuCreateExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	if _, err := r.recordingExecutor.Execute(ctx, cmd, args...); err != nil {
		return nil, err
	}
	if containsArg(args, "state") {
		return []byte(fmt.Sprintf(`{"status":"created","pid":%d}`, r.statePID)), r.stateErr
	}
	return nil, nil
}

func (r *cpuCreateExecutor) ExecuteWithIO(ctx context.Context, cmd, stdin, stdout, stderr string, args ...string) error {
	if err := r.recordingExecutor.ExecuteWithIO(ctx, cmd, stdin, stdout, stderr, args...); err != nil {
		return err
	}
	return r.createErr
}

func TestRunscCreateVerifiesCPUOnlyControlsBeforeReturning(t *testing.T) {
	readbackErr := errors.New("runtime changed CPU quota")
	createErr := errors.New("runtime create failed")
	stateErr := errors.New("runtime state unavailable")
	processErr := errors.New("gofer is outside the allocated cgroup")
	for _, test := range []struct {
		name          string
		disabled      bool
		noCPU         bool
		noPID         bool
		createErr     error
		verifyErr     error
		stateErr      error
		processErr    error
		wantErr       error
		wantText      string
		wantVerify    bool
		wantProcesses bool
	}{
		{name: "required CPU only", wantVerify: true, wantProcesses: true},
		{name: "runtime changed controls", verifyErr: readbackErr, wantErr: readbackErr, wantVerify: true},
		{name: "failed runtime create", createErr: createErr, wantErr: createErr},
		{name: "unreadable runtime state", stateErr: stateErr, wantErr: stateErr, wantVerify: true},
		{name: "missing Sentry PID", noPID: true, wantText: "no Sentry host pid", wantVerify: true},
		{name: "incorrect process attribution", processErr: processErr, wantErr: processErr, wantVerify: true, wantProcesses: true},
		{name: "disabled_dev", disabled: true},
		{name: "no CPU controls", noCPU: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rootDir := t.TempDir()
			loader, err := runtimeoci.NewBundleLoader(filepath.Join(rootDir, "containers"))
			if err != nil {
				t.Fatal(err)
			}
			handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
			if err != nil {
				t.Fatal(err)
			}
			handler.ignoreCgroups = test.disabled
			executor := &cpuCreateExecutor{createErr: test.createErr, stateErr: test.stateErr, statePID: 1234}
			if test.noPID {
				executor.statePID = 0
			}
			handler.common.SetExecutor(executor)
			resource := &apipb.LinuxContainerResources{CpuShares: 1024, CpuQuota: 25000, CpuPeriod: 100000}
			if test.noCPU {
				resource = nil
			}
			verified := false
			processesVerified := false
			previous := verifyCreatedCPUControls
			previousProcesses := verifyCreatedCPUProcesses
			t.Cleanup(func() {
				verifyCreatedCPUControls = previous
				verifyCreatedCPUProcesses = previousProcesses
			})
			verifyCreatedCPUControls = func(path string, got *apipb.LinuxContainerResources) error {
				commands := executor.Args()
				if len(commands) != 1 || !containsArg(commands[0], "create") {
					t.Fatalf("CPU readback did not follow runtime create: %v", commands)
				}
				if path != "/sandbox/cpu-only/workload" || got != resource || got.GetMemoryLimitInBytes() != 0 {
					t.Fatalf("CPU-only readback changed leaf or resource: %s %v", path, got)
				}
				verified = true
				return test.verifyErr
			}
			verifyCreatedCPUProcesses = func(path string, pid int, binary string) error {
				if !verified || path != "/sandbox/cpu-only/workload" || pid != executor.statePID || binary != "/usr/local/bin/runsc" {
					t.Fatalf("process verification changed CPU leaf or runtime identity: verified=%t path=%s pid=%d binary=%s", verified, path, pid, binary)
				}
				processesVerified = true
				return test.processErr
			}
			err = handler.createPreparedContainer(context.Background(), "", "", filepath.Join(rootDir, "bundle"), "cpu-only", nil, resource,
				contract.HandlerOptions{ContainerID: "cpu-only", RuntimeCgroupPath: "/sandbox/cpu-only/workload"})
			if test.wantText != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantText) {
					t.Fatalf("error = %v, want %q", err, test.wantText)
				}
			} else if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if verified != test.wantVerify || processesVerified != test.wantProcesses {
				t.Fatalf("create CPU fence: verified=%t error=%v; want verified=%t error=%v", verified, err, test.wantVerify, test.wantErr)
			}
			for _, command := range executor.Args() {
				if containsArg(command, "start") || containsArg(command, "run") {
					t.Fatalf("user code started during CPU preparation: %v", command)
				}
			}
		})
	}
}
