package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type recoveryRuntime struct {
	*runtimetest.FakeSandboxRuntime
	mu                      sync.Mutex
	states                  map[string]contract.ContainerStatus
	resumed, paused, killed []string
	killHook                func(string)
	resumeHook              func(string)
	killErr                 error
	resumeErrID             string
	pauseHook               func(string)
	workloadStops           int
	kernelStopped           bool
}

func (r *recoveryRuntime) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*contract.UnionContainerState
	for id, status := range r.states {
		result = append(result, &contract.UnionContainerState{ID: id, Status: status, InitProcessPid: 1234})
	}
	return result, nil
}
func (r *recoveryRuntime) PauseContainer(_ context.Context, o contract.HandlerOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pauseHook != nil {
		r.pauseHook(o.ContainerID)
	}
	r.paused = append(r.paused, o.ContainerID)
	r.states[o.ContainerID] = contract.ContainerStatusPaused
	return nil
}

func (r *recoveryRuntime) StopWorkload(ctx context.Context, o contract.HandlerOptions) (contract.Exit, error) {
	r.mu.Lock()
	r.workloadStops++
	r.mu.Unlock()
	return r.FakeSandboxRuntime.StopWorkload(ctx, o)
}
func (r *recoveryRuntime) ResumeContainer(_ context.Context, o contract.HandlerOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resumeHook != nil {
		r.resumeHook(o.ContainerID)
	}
	if o.ContainerID == r.resumeErrID {
		return errors.New("resume rejected")
	}
	r.resumed = append(r.resumed, o.ContainerID)
	r.states[o.ContainerID] = contract.ContainerStatusRunning
	return nil
}
func (r *recoveryRuntime) KillSandbox(_ context.Context, o contract.HandlerOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.killHook != nil {
		r.killHook(o.ContainerID)
	}
	if r.killErr != nil {
		return r.killErr
	}
	r.killed = append(r.killed, o.ContainerID)
	r.states[o.ContainerID] = contract.ContainerStatusExited
	return nil
}
func (r *recoveryRuntime) Wait(context.Context, contract.HandlerOptions) (contract.Exit, error) {
	return contract.Exit{}, contract.ErrExitStatusUnavailable
}

func newRecoveryRuntime() *recoveryRuntime {
	return &recoveryRuntime{FakeSandboxRuntime: runtimetest.NewFakeSandboxRuntime(), states: map[string]contract.ContainerStatus{}}
}

func persistRecoveryAllocation(t *testing.T, s *sandboxService, id string, deadline time.Time) *apipb.AllocationState {
	t.Helper()
	record := &apipb.AllocationState{
		AllocationID: id, NodeID: "node-a", AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExecutionLeaseExpiresAtUnixNano: deadline.UnixNano(),
		Environment:                     &apipb.ResolvedEnvironment{ID: "environment-" + id, Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}}, Argv: []string{"/bin/sh"}},
		EnforcementManifest:             &apipb.AllocationEnforcementManifest{BundlePath: filepath.Join(s.config.RootDir, "containers", id), CreatedAtUnixNano: time.Now().Add(-time.Minute).UnixNano()},
	}
	require.NoError(t, s.store.PutRecord(config.AllocationStateBucket, id, record))
	require.NoError(t, s.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
	return record
}

func TestRecoveryPersistsExpiryBeforeHostFailStopAndPreservesFirstCause(t *testing.T) {
	for _, prior := range []commonv1.WorkloadDiagnosticCode{commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST} {
		t.Run(prior.String(), func(t *testing.T) {
			r := newRecoveryRuntime()
			r.states["allocation"] = contract.ContainerStatusPaused
			s := newRecoveryTestService(t, r)
			record := persistRecoveryAllocation(t, s, "allocation", time.Now().Add(-time.Second))
			record.TerminationDiagnosticCode = prior
			if prior != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
				record.TerminationMessage = "original reason"
			}
			require.NoError(t, s.store.PutRecord(config.AllocationStateBucket, "allocation", record))
			want := prior
			if want == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
				want = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED
			}
			r.killHook = func(id string) {
				var stored apipb.AllocationState
				require.NoError(t, s.store.GetRecord(config.AllocationStateBucket, id, &stored))
				require.Equal(t, want, stored.TerminationDiagnosticCode, "authority must be durable before host signal")
			}
			inventory, err := s.collectRuntimeInventory(t.Context())
			require.NoError(t, err)
			require.NoError(t, s.stopUnauthorizedRecoveredSandboxes(t.Context(), inventory))
			require.Equal(t, contract.ContainerStatusExited, inventory["allocation"].Status)
			require.False(t, s.allocations.HasAllocation("allocation"), "expiry inspection must not pretend resource ownership was restored")
			require.NoError(t, s.recoverTerminalRuntimeCheckpoints(t.Context(), inventory))
			item, err := s.containerManager.Get("allocation")
			require.NoError(t, err)
			require.Equal(t, want, item.Status.Get().DiagnosticCode)
			require.Nil(t, item.Status.Get().ExitCode, "whole-sandbox fail-stop cannot invent supervised workload exit status")
			require.Empty(t, r.resumed)
		})
	}
}

func TestRecoveryHostFailStopFailureRetainsPausedInventoryAndAuthority(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	r.killErr = errors.New("cgroup identity changed")
	s := newRecoveryTestService(t, r)
	persistRecoveryAllocation(t, s, "allocation", time.Now().Add(-time.Second))
	inventory, err := s.collectRuntimeInventory(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, s.stopUnauthorizedRecoveredSandboxes(t.Context(), inventory), r.killErr)
	require.Equal(t, contract.ContainerStatusPaused, inventory["allocation"].Status)
	code, _ := s.allocations.TerminationIntent("allocation")
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, code)
}

func TestResumeRequiresWatchdogAndCompleteRecoveredOwnership(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	s := newRecoveryTestService(t, r)
	persistRecoveryAllocation(t, s, "allocation", time.Now().Add(time.Minute))
	require.ErrorContains(t, s.resumeRecoveredSandboxes(t.Context()), "without execution lease watchdog")
	s.config.ControlPlaneTarget = "127.0.0.1:1"
	s.startExecutionLeaseWatchdog(t.Context())
	defer s.executionLeaseCancel()
	require.ErrorContains(t, s.resumeRecoveredSandboxes(t.Context()), "incomplete recovered execution ownership")
	require.Empty(t, r.resumed)
	require.NoError(t, s.allocations.RestoreAllocationState(map[string]struct{}{"allocation": {}}))
	r.resumeHook = func(string) { require.NotNil(t, s.executionLeaseCancel) }
	require.NoError(t, s.resumeRecoveredSandboxes(t.Context()))
	require.Equal(t, []string{"allocation"}, r.resumed)
}

func TestResumeRechecksElapsedLeaseAndRejectsLateRenewal(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	s := newRecoveryTestService(t, r)
	record := persistRecoveryAllocation(t, s, "allocation", time.Now().Add(-time.Second))
	require.NoError(t, s.allocations.RestoreAllocationState(map[string]struct{}{"allocation": {}}))
	s.config.ControlPlaneTarget = "127.0.0.1:1"
	s.startExecutionLeaseWatchdog(t.Context())
	defer s.executionLeaseCancel()
	require.NoError(t, s.resumeRecoveredSandboxes(t.Context()))
	require.Empty(t, r.resumed)
	require.Equal(t, []string{"allocation"}, r.killed)
	require.NoError(t, s.allocations.RenewExecutionLeases(map[string]time.Duration{"allocation": time.Minute}, time.Now()))
	var after apipb.AllocationState
	require.NoError(t, s.store.GetRecord(config.AllocationStateBucket, "allocation", &after))
	require.Equal(t, record.ExecutionLeaseExpiresAtUnixNano, after.ExecutionLeaseExpiresAtUnixNano)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, after.TerminationDiagnosticCode)
	require.Error(t, s.allocations.ValidateActivation("allocation"))
}

func TestUnambiguousResumeFailureRepausesEarlierAllocationsBeforeRunReturns(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["first"] = contract.ContainerStatusPaused
	r.states["second"] = contract.ContainerStatusPaused
	r.resumeErrID = "second"
	s := newRecoveryTestService(t, r)
	for _, id := range []string{"first", "second"} {
		persistRecoveryAllocation(t, s, id, time.Now().Add(time.Minute))
	}
	require.NoError(t, s.allocations.RestoreAllocationState(map[string]struct{}{"first": {}, "second": {}}))
	s.config.ControlPlaneTarget = "127.0.0.1:1"
	require.ErrorContains(t, s.Run(t.Context()), "resume rejected")
	defer s.executionLeaseCancel()
	require.False(t, s.Ready())
	require.False(t, s.recoveryComplete.Load())
	require.Equal(t, []string{"first"}, r.resumed)
	require.Equal(t, []string{"first"}, r.paused)
	require.Equal(t, contract.ContainerStatusPaused, r.states["first"])
}

func newRecoveryTestService(t *testing.T, r contract.SandboxRuntime) *sandboxService {
	s := newTestServiceWithPreparedEnvironmentManager(t, r, environmentcache.NewEnvironmentCache(environmentcache.NewDefaultMounter(false, "")))
	s.config.RuntimeConfig.NativeRecovery = true
	return s
}

type recoveryWriteFailureStore struct{ nodeStateStore }

func (s recoveryWriteFailureStore) PutRecord(bucket, key string, value proto.Message) error {
	if bucket == config.AllocationStateBucket {
		return errors.New("execution authority write unavailable")
	}
	return s.nodeStateStore.PutRecord(bucket, key, value)
}

func TestRecoveryWriteFailureCannotSignalOrResumeSandbox(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	s := newRecoveryTestService(t, r)
	persistRecoveryAllocation(t, s, "allocation", time.Now().Add(-time.Second))
	s.store = recoveryWriteFailureStore{s.store}
	s.allocations = nil
	s.configureAllocationController()
	inventory, err := s.collectRuntimeInventory(t.Context())
	require.NoError(t, err)
	require.ErrorContains(t, s.stopUnauthorizedRecoveredSandboxes(t.Context(), inventory), "execution authority write unavailable")
	require.Empty(t, r.killed)
	require.Empty(t, r.resumed)
	require.Equal(t, contract.ContainerStatusPaused, inventory["allocation"].Status)
}

func TestPausedLeaseWatchdogUsesHostFailStopAndRetainsCleanupState(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	s := newRecoveryTestService(t, r)
	deadline := time.Now().Add(time.Minute)
	persistRecoveryAllocation(t, s, "allocation", deadline)
	require.NoError(t, s.allocations.RestoreAllocationState(map[string]struct{}{"allocation": {}}))
	s.stopExpiredExecutionLeases(t.Context(), deadline)
	require.Equal(t, []string{"allocation"}, r.killed)
	require.Zero(t, r.workloadStops, "paused sandboxd cannot serve stop requests")
	require.True(t, s.allocations.HasAllocation("allocation"))
}

func TestShutdownPausesBeforeCancelingLeaseWatchdog(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusRunning
	s := newRecoveryTestService(t, r)
	canceled := false
	s.executionLeaseCancel = func() { canceled = true }
	r.pauseHook = func(string) {
		require.False(t, canceled, "shutdown must pause before disabling finite-lease enforcement")
	}
	require.NoError(t, s.Shutdown(t.Context()))
	require.True(t, canceled)
	require.Equal(t, []string{"allocation"}, r.paused)
}

func (r *recoveryRuntime) VerifySandboxStopped(context.Context, contract.HandlerOptions) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kernelStopped, nil
}

func TestDeadPausedRuntimeIsRecoveredBeforeItsLeaseExpires(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	r.kernelStopped = true
	s := newRecoveryTestService(t, r)
	persistRecoveryAllocation(t, s, "allocation", time.Now().Add(time.Minute))
	inventory, err := s.collectRuntimeInventory(t.Context())
	require.NoError(t, err)
	require.NoError(t, s.stopUnauthorizedRecoveredSandboxes(t.Context(), inventory))
	require.Equal(t, contract.ContainerStatusExited, inventory["allocation"].Status)
	require.Empty(t, r.killed)
	require.Empty(t, r.resumed)
	require.NoError(t, s.recoverTerminalRuntimeCheckpoints(t.Context(), inventory))
	item, err := s.containerManager.Get("allocation")
	require.NoError(t, err)
	require.Equal(t, apipb.ContainerState_CONTAINER_EXITED, item.Status.Get().State())
	require.Nil(t, item.Status.Get().ExitCode)
}

func TestExecutionLeaseWatchdogSurvivesCanceledServeContextUntilExplicitStop(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusPaused
	s := newRecoveryTestService(t, r)
	persistRecoveryAllocation(t, s, "allocation", time.Now().Add(50*time.Millisecond))
	require.NoError(t, s.allocations.RestoreAllocationState(map[string]struct{}{"allocation": {}}))
	s.config.ControlPlaneTarget = "127.0.0.1:1"
	stopped := make(chan struct{}, 1)
	r.killHook = func(string) {
		select {
		case stopped <- struct{}{}:
		default:
		}
	}
	serveCtx, cancelServe := context.WithCancel(t.Context())
	s.startExecutionLeaseWatchdog(serveCtx)
	defer func() { s.executionLeaseCancel(); s.executionLeaseWG.Wait() }()
	cancelServe()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("serve cancellation disabled lease enforcement before shutdown paused guests")
	}
}

func TestDefaultProfilePreservesShutdownAndRejectsPausedAdoption(t *testing.T) {
	r := newRecoveryRuntime()
	r.states["allocation"] = contract.ContainerStatusRunning
	s := newTestService(t, r)
	require.False(t, s.config.RuntimeConfig.NativeRecovery)
	require.NoError(t, s.pauseLiveSandboxes(t.Context()))
	require.Empty(t, r.paused)
	r.states["allocation"] = contract.ContainerStatusPaused
	require.ErrorContains(t, s.resumeRecoveredSandboxes(t.Context()), "requires native_recovery")
	inventory, err := s.collectRuntimeInventory(t.Context())
	require.NoError(t, err)
	require.ErrorContains(t, s.stopUnauthorizedRecoveredSandboxes(t.Context(), inventory), "requires native_recovery")
	require.Empty(t, r.resumed)
	require.Empty(t, r.killed)
}
