package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

type networkRecoveryProbe struct {
	calls int
	live  map[string]int
	err   error
}

func (p *networkRecoveryProbe) AllocationResource(string) (string, bool) { return "binding", true }

func (p *networkRecoveryProbe) VerifyRecoveredNetworkBindings(live map[string]int) error {
	p.calls++
	p.live = live
	return p.err
}

func TestNetworkRecoveryAuditsRunningAndPausedRuntimeIdentities(t *testing.T) {
	probe := &networkRecoveryProbe{}
	service := &sandboxService{networkRecovery: probe, runscHandler: &networkProjectionRuntime{}}
	inventory := runtimeInventory{
		"running": {ID: "running", Status: contract.ContainerStatusRunning, InitProcessPid: 101},
		"paused":  {ID: "paused", Status: contract.ContainerStatusPaused, InitProcessPid: 102},
		"exited":  {ID: "exited", Status: contract.ContainerStatusExited},
	}
	require.NoError(t, service.verifyRecoveredNetworkBindings(inventory))
	require.Equal(t, map[string]int{"running": 101, "paused": 102}, probe.live)
	mismatch := errors.New("runtime namespace differs from durable lease")
	probe.err = mismatch
	require.ErrorIs(t, service.verifyRecoveredNetworkBindings(inventory), mismatch)
	require.Equal(t, contract.ContainerStatusPaused, inventory["paused"].Status)
	require.Equal(t, 102, inventory["paused"].InitProcessPid)
}

func TestNetworkRecoveryRejectsUncertainRuntimeBeforeAudit(t *testing.T) {
	for _, state := range []*contract.UnionContainerState{
		nil,
		{ID: "allocation", Status: contract.ContainerStatusUnknown},
		{ID: "allocation", Status: contract.ContainerStatusRunning},
		{ID: "allocation", Status: contract.ContainerStatusPaused, InitProcessPid: -1},
	} {
		probe := &networkRecoveryProbe{}
		service := &sandboxService{networkRecovery: probe, runscHandler: &networkProjectionRuntime{}}
		require.Error(t, service.verifyRecoveredNetworkBindings(runtimeInventory{"allocation": state}))
		require.Zero(t, probe.calls)
	}
}

func TestContainerReadinessRequiresCompletedRecovery(t *testing.T) {
	service := &sandboxService{}
	health := make(chan bool)
	service.watchContainerReadiness(health)
	health <- true
	health <- true // synchronize the first store without sleeps
	require.False(t, service.ready.Load())
	service.recoveryComplete.Store(true)
	health <- true
	health <- true
	require.True(t, service.ready.Load())
	service.recoveryComplete.Store(false)
	health <- true
	health <- true
	require.False(t, service.ready.Load())
	close(health)
	// A health update may already have sampled the old recovery flag when
	// shutdown closes it. The public readiness gate must reject a stale value.
	service.ready.Store(true)
	require.False(t, service.Ready())
}

type networkProjectionRuntime struct {
	contract.SandboxRuntime
	path       string
	namespaces []spec.LinuxNamespace
	calls      int
}

func (r *networkProjectionRuntime) ContainerSpec(context.Context, contract.HandlerOptions) (*spec.Spec, error) {
	r.calls++
	namespaces := r.namespaces
	if namespaces == nil {
		namespaces = []spec.LinuxNamespace{{Type: spec.NetworkNamespace, Path: r.path}}
	}
	return &spec.Spec{Linux: &spec.Linux{Namespaces: namespaces}}, nil
}

type missingNetworkLease struct{ *networkRecoveryProbe }

func (p missingNetworkLease) AllocationResource(string) (string, bool) { return "", false }

func TestNetworkRecoveryRejectsMissingLeaseWithoutTrustingProjection(t *testing.T) {
	p := &networkRecoveryProbe{}
	s := &sandboxService{networkRecovery: missingNetworkLease{p}, runscHandler: &networkProjectionRuntime{path: "/run/netns/assigned"}}
	require.ErrorContains(t, s.verifyRecoveredNetworkBindings(runtimeInventory{"allocation": {ID: "allocation", Status: contract.ContainerStatusPaused, InitProcessPid: 11}}), "without a durable lease")
	require.Zero(t, p.calls)
	s.networkRecovery = nil
	s.config.InterfaceCacheSize = 8
	require.ErrorContains(t, s.verifyRecoveredNetworkBindings(nil), "verifier is unavailable")
}

func TestNetworkRecoveryRejectsMissingOrDuplicateNetworkNamespaceWithoutMutation(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []spec.LinuxNamespace
	}{
		{"empty namespace list", []spec.LinuxNamespace{}},
		{"no network namespace", []spec.LinuxNamespace{{Type: spec.MountNamespace}}},
		{"duplicate private namespaces", []spec.LinuxNamespace{{Type: spec.NetworkNamespace}, {Type: spec.NetworkNamespace}}},
		{"duplicate connected namespaces", []spec.LinuxNamespace{{Type: spec.NetworkNamespace, Path: "/run/netns/assigned"}, {Type: spec.NetworkNamespace, Path: "/run/netns/other"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := &networkRecoveryProbe{}
			runtime := &networkProjectionRuntime{namespaces: test.namespaces}
			service := &sandboxService{networkRecovery: probe, runscHandler: runtime}
			state := &contract.UnionContainerState{ID: "allocation", Status: contract.ContainerStatusPaused, InitProcessPid: 123}
			beforeState := *state
			beforeNamespaces := append([]spec.LinuxNamespace{}, test.namespaces...)
			require.ErrorContains(t, service.verifyRecoveredNetworkBindings(runtimeInventory{"allocation": state}), "exactly one private network namespace")
			require.Zero(t, probe.calls, "an invalid projection must not reach the kernel audit")
			require.Equal(t, 1, runtime.calls)
			require.Equal(t, beforeState, *state)
			require.Equal(t, beforeNamespaces, runtime.namespaces)
		})
	}
}

func TestNetworkRecoveryRejectsUnknownRuntimeBeforeReadingProjection(t *testing.T) {
	probe := &networkRecoveryProbe{}
	runtime := &networkProjectionRuntime{}
	service := &sandboxService{networkRecovery: probe, runscHandler: runtime}
	service.config.InterfaceCacheSize = 8
	state := &contract.UnionContainerState{ID: "allocation", Status: contract.ContainerStatusUnknown, InitProcessPid: 123}
	before := *state
	require.ErrorContains(t, service.verifyRecoveredNetworkBindings(runtimeInventory{"allocation": state}), "uncertain runtime state")
	require.Zero(t, runtime.calls)
	require.Zero(t, probe.calls)
	require.Equal(t, before, *state)
	require.Equal(t, 8, service.config.InterfaceCacheSize)
}
