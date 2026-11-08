package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type taskControlExecutor struct {
	recordingExecutor
	controls    map[string]string
	readErr     string
	writeErr    string
	ignoreWrite string
	status      string
	pid         int
}

func (e *taskControlExecutor) Execute(ctx context.Context, binary string, args ...string) ([]byte, error) {
	_, _ = e.recordingExecutor.Execute(ctx, binary, args...)
	if containsArg(args, "state") {
		return []byte(fmt.Sprintf(`{"status":%q,"pid":%d}`, e.status, e.pid)), nil
	}
	for i, arg := range args {
		if arg != "read-control" && arg != "write-control" {
			continue
		}
		if len(args) < i+5 || args[i+1] != "task-allocation" || args[i+2] != "pids" || args[i+3] != "/task-allocation" {
			return nil, fmt.Errorf("incorrect guest controller identity: %v", args)
		}
		name := args[i+4]
		if arg == "read-control" {
			if e.readErr == name {
				return nil, errors.New("guest read failed")
			}
			return []byte(e.controls[name] + "\n"), nil
		}
		if len(args) != i+6 {
			return nil, fmt.Errorf("incorrect write arguments: %v", args)
		}
		if e.writeErr == name {
			return nil, errors.New("guest write failed")
		}
		if e.ignoreWrite != name {
			e.controls[name] = args[i+5]
		}
		return nil, nil
	}
	return nil, nil
}

func taskEnforcementFixture(t *testing.T) (*RunscServiceHandler, *apipb.AllocationEnforcementManifest, *taskControlExecutor) {
	t.Helper()
	root := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader(filepath.Join(root, "containers"))
	require.NoError(t, err)
	cfg := config.Config{RootDir: root, PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{GuestTaskLimit: 256, HostTaskLimit: 1024}}}
	handler, err := NewRunscServiceHandler(cfg, config.RuntimeInstanceConfig{Binary: "/fake/runsc"}, loader)
	require.NoError(t, err)
	t.Cleanup(handler.ShutDown)
	manifest := &apipb.AllocationEnforcementManifest{
		ProcessSecurityProfile: wire.ProcessSecurityProfile,
		BundlePath:             filepath.Join(root, "containers", "task-allocation"), CreatedAtUnixNano: 1,
		GuestTaskLimit: 256, HostTaskLimit: 1024,
		CgroupPath: "/sandbox/task-allocation", RuntimeCgroupPath: "/sandbox/task-allocation/workload",
	}
	writeTaskManifestFixture(t, manifest)
	executor := &taskControlExecutor{controls: map[string]string{
		"pids.max": "256", "pids.current": "5", "cgroup.max.depth": "0", "cgroup.max.descendants": "0",
		"cgroup.stat": "nr_descendants 0\nnr_dying_descendants 0\n",
	}, status: "running", pid: 1234}
	handler.common.SetExecutor(executor)
	previousDomain, previousProcesses := verifyTaskPIDDomain, verifyTaskProcesses
	t.Cleanup(func() { verifyTaskPIDDomain, verifyTaskProcesses = previousDomain, previousProcesses })
	verifyTaskPIDDomain = func(parent, leaf string, limit int64) error {
		if parent != manifest.CgroupPath || leaf != manifest.RuntimeCgroupPath || limit != manifest.HostTaskLimit {
			return fmt.Errorf("changed durable host policy: %s %s %d", parent, leaf, limit)
		}
		return nil
	}
	verifyTaskProcesses = func(path string, pid int, binary string) error {
		if path != manifest.RuntimeCgroupPath || pid != 1234 || binary != "/fake/runsc" {
			return errors.New("incorrect runtime attribution")
		}
		return nil
	}
	return handler, manifest, executor
}

func writeTaskManifestFixture(t *testing.T, manifest *apipb.AllocationEnforcementManifest) {
	t.Helper()
	require.NoError(t, os.MkdirAll(manifest.BundlePath, 0o700))
	data, err := proto.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(manifest.BundlePath, runtimeEnforcementManifestFile), data, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(manifest.BundlePath, "config.json"), []byte(`{"process":{"noNewPrivileges":true,"capabilities":{}}}`), 0o600))
}

func TestConfigureGuestTaskLimitsClosesDelegationBeforeReleasingTaskCeiling(t *testing.T) {
	handler, manifest, executor := taskEnforcementFixture(t)
	executor.controls["pids.max"] = "max"
	executor.controls["cgroup.max.depth"] = "max"
	executor.controls["cgroup.max.descendants"] = "max"
	require.NoError(t, handler.configureGuestTaskLimits(t.Context(), "task-allocation", manifest))
	var writes []string
	for _, args := range executor.Args() {
		for i, arg := range args {
			if arg == "write-control" {
				writes = append(writes, strings.Join(args[i+4:], " "))
			}
		}
	}
	require.Equal(t, []string{"cgroup.max.depth 0", "cgroup.max.descendants 0", "pids.max 256"}, writes)
}

func TestConfigureGuestTaskLimitsFailsBeforeTaskCeilingOnIncompleteProtection(t *testing.T) {
	for _, test := range []struct{ name, writeErr, readErr, ignoreWrite, stat string }{
		{name: "depth write", writeErr: "cgroup.max.depth"},
		{name: "depth read", readErr: "cgroup.max.depth"},
		{name: "depth readback", ignoreWrite: "cgroup.max.depth"},
		{name: "descendant write", writeErr: "cgroup.max.descendants"},
		{name: "existing child", stat: "nr_descendants 1\nnr_dying_descendants 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, manifest, executor := taskEnforcementFixture(t)
			executor.controls["pids.max"] = "max"
			executor.controls["cgroup.max.depth"] = "max"
			executor.writeErr, executor.readErr, executor.ignoreWrite = test.writeErr, test.readErr, test.ignoreWrite
			if test.stat != "" {
				executor.controls["cgroup.stat"] = test.stat
			}
			require.Error(t, handler.configureGuestTaskLimits(t.Context(), "task-allocation", manifest))
			require.Equal(t, "max", executor.controls["pids.max"], "PID 1 must remain behind its task ceiling gate")
		})
	}
}

func TestRecoveredTaskLimitsUseDurablePolicyAfterNodeConfigChanges(t *testing.T) {
	for _, configured := range []int64{0, 512} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			handler, manifest, executor := taskEnforcementFixture(t)
			handler.guestTaskLimit, handler.hostTaskLimit = configured, configured*4
			require.NoError(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest))
			for _, args := range executor.Args() {
				require.NotContains(t, args, "write-control", "recovery must not rewrite limits")
			}
		})
	}
}

func TestRecoveredTaskLimitsRejectMissingOrChangedEvidence(t *testing.T) {
	for _, test := range []struct{ name, control, value string }{
		{"unlimited", "pids.max", "max"}, {"wrong limit", "pids.max", "512"},
		{"writable descendants", "cgroup.max.descendants", "max"}, {"writable depth", "cgroup.max.depth", "1"},
		{"too many tasks", "pids.current", "257"}, {"invalid tasks", "pids.current", "5 6"},
		{"missing tasks", "pids.current", ""}, {"missing stat", "cgroup.stat", ""},
		{"existing child", "cgroup.stat", "nr_descendants 1\nnr_dying_descendants 0"},
		{"duplicate stat", "cgroup.stat", "nr_descendants 0\nnr_descendants 0\nnr_dying_descendants 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, manifest, executor := taskEnforcementFixture(t)
			executor.controls[test.control] = test.value
			require.Error(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest))
			for _, args := range executor.Args() {
				require.NotContains(t, args, "write-control")
			}
		})
	}
	t.Run("pre-policy runtime", func(t *testing.T) {
		handler, manifest, executor := taskEnforcementFixture(t)
		manifest.GuestTaskLimit, manifest.HostTaskLimit = 0, 0
		require.ErrorContains(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest), "predates task enforcement")
		require.Empty(t, executor.Args())
	})
	t.Run("projection differs", func(t *testing.T) {
		handler, manifest, _ := taskEnforcementFixture(t)
		changed := proto.Clone(manifest).(*apipb.AllocationEnforcementManifest)
		changed.GuestTaskLimit = 512
		writeTaskManifestFixture(t, changed)
		require.ErrorContains(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest), "differs from durable")
	})
	t.Run("host changed", func(t *testing.T) {
		handler, manifest, executor := taskEnforcementFixture(t)
		verifyTaskPIDDomain = func(string, string, int64) error { return errors.New("host unlimited") }
		require.ErrorContains(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest), "host unlimited")
		for _, args := range executor.Args() {
			require.NotContains(t, args, "write-control")
		}
	})
	t.Run("runtime attribution changed", func(t *testing.T) {
		handler, manifest, _ := taskEnforcementFixture(t)
		verifyTaskProcesses = func(string, int, string) error { return errors.New("wrong gofer") }
		require.ErrorContains(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest), "wrong gofer")
	})
}

func TestTaskPolicyChangesRuntimeDigestAndCreateFlags(t *testing.T) {
	handler, _, _ := taskEnforcementFixture(t)
	before, err := handler.ConfigurationDigest()
	require.NoError(t, err)
	args, err := handler.preparedContainerCreateArgs(contract.HandlerOptions{}, nil, "/pid", "/bundle", "task-allocation")
	require.NoError(t, err)
	require.Contains(t, args, "--in-sandbox-cgroup=v2")
	handler.guestTaskLimit = 512
	after, err := handler.ConfigurationDigest()
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	handler.guestTaskLimit, handler.hostTaskLimit = 0, 0
	args, err = handler.preparedContainerLifecycleArgs(contract.HandlerOptions{}, "task-allocation", "start", "task-allocation")
	require.NoError(t, err)
	require.NotContains(t, args, "--in-sandbox-cgroup=v2")
}

func TestTaskPolicyOverridesBothCallerProfilesWithoutMutation(t *testing.T) {
	handler, _, _ := taskEnforcementFixture(t)
	caller := runtimeoci.DefaultExecutionProfile()
	caller.Baseline.NoFileLimit = 2048
	template := &runtimeoci.TemplateOptions{ExecutionProfile: &caller}
	options := handler.withTaskExecutionProfiles(contract.HandlerOptions{ExecutionProfile: &caller, BundleTemplateSource: template})
	for _, profile := range []*runtimeoci.ExecutionProfile{options.ExecutionProfile, options.BundleTemplateSource.ExecutionProfile} {
		require.Equal(t, int64(256), profile.GuestTaskLimit)
		require.Equal(t, int64(1024), profile.HostTaskLimit)
		require.Equal(t, uint64(2048), profile.Baseline.NoFileLimit)
	}
	require.Zero(t, caller.GuestTaskLimit)
	require.Same(t, &caller, template.ExecutionProfile)
	require.NotSame(t, template, options.BundleTemplateSource)
}

func TestRunscCreateChecksTaskOnlyHostBoundaryBeforeReturning(t *testing.T) {
	handler, manifest, executor := taskEnforcementFixture(t)
	executor.status = "created"
	verified := false
	verifyTaskPIDDomain = func(parent, leaf string, limit int64) error {
		require.Equal(t, 1, executor.ioCalls, "host fence must follow runsc create")
		require.Equal(t, manifest.CgroupPath, parent)
		require.Equal(t, manifest.RuntimeCgroupPath, leaf)
		require.Equal(t, manifest.HostTaskLimit, limit)
		verified = true
		return nil
	}
	err := handler.createPreparedContainer(t.Context(), "", "", manifest.BundlePath, "task-allocation", nil, nil,
		contract.HandlerOptions{ContainerID: "task-allocation", CgroupPath: manifest.CgroupPath, RuntimeCgroupPath: manifest.RuntimeCgroupPath})
	require.NoError(t, err)
	require.True(t, verified)
	for _, args := range executor.Args() {
		require.NotContains(t, args, "start")
	}
}
