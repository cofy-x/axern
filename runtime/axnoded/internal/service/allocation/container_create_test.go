package allocation

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestCreateRuntimeContainerPreservesFastExitStatus(t *testing.T) {
	const containerID = "axctl-create-fast-exit"
	releaseExit := make(chan struct{})
	waitEntered := make(chan struct{})
	handler := &runtimeSpyHandler{
		name: "runsc",
		waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
			close(waitEntered)
			select {
			case <-releaseExit:
				return contract.Exit{Status: 42, Timestamp: time.Now().UTC()}, nil
			case <-ctx.Done():
				return contract.Exit{}, ctx.Err()
			}
		},
		listStates: []*contract.UnionContainerState{{
			ID:             containerID,
			InitProcessPid: 321,
			Status:         contract.ContainerStatusExited,
			Created:        "2026-08-11T15:59:37Z",
		}},
		listHook: func() {
			select {
			case <-waitEntered:
			case <-time.After(time.Second):
				t.Fatal("runtime state was listed before the Wait observer started")
			}
		},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})

	resp, _, err := fixture.controller.CreateRuntimeContainer(context.Background(), nil, nil, &apipb.CreateContainerRequest{
		ID:      containerID,
		Runtime: "runsc",
	}, nil, nil)
	if err != nil {
		t.Fatalf("CreateRuntimeContainer() error = %v", err)
	}
	if resp.GetID() != containerID {
		t.Fatalf("container id = %q, want %q", resp.GetID(), containerID)
	}
	created, err := fixture.manager.Get(containerID)
	if err != nil {
		t.Fatalf("Get(%q) before exact exit: %v", containerID, err)
	}
	createdStatus := created.Status.Get()
	if createdStatus.FinishedAt != "" || createdStatus.Message != "" || createdStatus.ExitCodeKnown {
		t.Fatalf("lossy runtime list published a terminal status before Wait proof: %+v", createdStatus)
	}
	if createdStatus.Pid != 0 || createdStatus.StartedAt == "2026-08-11T15:59:37Z" {
		t.Fatalf("exited runtime list revived stale process identity: %+v", createdStatus)
	}
	close(releaseExit)
	assertExactContainerExit(t, fixture, containerID, 42)
}

func TestCreateRuntimeContainerSyncsRuntimeStateIntoStatus(t *testing.T) {
	runtimeExited := make(chan struct{})
	t.Cleanup(func() { close(runtimeExited) })
	handler := &runtimeSpyHandler{
		name: "runc",
		waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
			select {
			case <-runtimeExited:
				return contract.Exit{Status: 0, Timestamp: time.Now().UTC()}, nil
			case <-ctx.Done():
				return contract.Exit{}, ctx.Err()
			}
		},
		listStates: []*contract.UnionContainerState{
			{
				ID:             "axctl-create-sync",
				InitProcessPid: 196,
				Status:         contract.ContainerStatusRunning,
				Created:        "2026-04-23T10:58:52.096323739Z",
			},
		},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": handler})

	resp, _, err := fixture.controller.CreateRuntimeContainer(context.Background(), nil, nil, &apipb.CreateContainerRequest{
		ID:      "axctl-create-sync",
		Runtime: "runc",
	}, nil, nil)
	if err != nil {
		t.Fatalf("CreateRuntimeContainer() error = %v", err)
	}
	if resp.GetID() != "axctl-create-sync" {
		t.Fatalf("container id = %q, want axctl-create-sync", resp.GetID())
	}

	c, err := fixture.manager.Get("axctl-create-sync")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	status := c.Status.Get()
	if status.Pid != 196 {
		t.Fatalf("Pid = %d, want 196", status.Pid)
	}
	if status.StartedAt != "2026-04-23T10:58:52.096323739Z" {
		t.Fatalf("StartedAt = %q, want runtime-created timestamp", status.StartedAt)
	}
}

func TestCreateRuntimeContainerIgnoresImageProcessResourceAnnotationOverride(t *testing.T) {
	const parentNetworkResource = "parent-net-resource"
	networkKey := resources.ResourceAnnotationKeyPrefix + string(resources.InterfaceResourceName)
	handler := &runtimeSpyHandler{name: "runc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": handler})

	_, _, err := fixture.controller.CreateRuntimeContainer(context.Background(), nil, nil, &apipb.CreateContainerRequest{
		ID:      "axctl-create-resource-override",
		Runtime: "runc",
		Labels: map[string]string{
			"axern.image_process.kind": "image_process",
			networkKey:                 parentNetworkResource,
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("CreateRuntimeContainer() error = %v", err)
	}

	if got := handler.lastOptions.AdditionalAnnotations[networkKey]; got == parentNetworkResource {
		t.Fatalf("network annotation unexpectedly accepted image process override %q", got)
	}
}

func TestCreateRuntimeContainerIgnoresUserResourceAnnotationOverride(t *testing.T) {
	const userNetworkResource = "user-net-resource"
	networkKey := resources.ResourceAnnotationKeyPrefix + string(resources.InterfaceResourceName)
	handler := &runtimeSpyHandler{name: "runc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": handler})

	_, _, err := fixture.controller.CreateRuntimeContainer(context.Background(), nil, nil, &apipb.CreateContainerRequest{
		ID:      "axctl-create-resource-no-override",
		Runtime: "runc",
		Labels:  map[string]string{networkKey: userNetworkResource},
	}, nil, nil)
	if err != nil {
		t.Fatalf("CreateRuntimeContainer() error = %v", err)
	}

	if got := handler.lastOptions.AdditionalAnnotations[networkKey]; got == userNetworkResource {
		t.Fatalf("network annotation unexpectedly accepted user override %q", got)
	}
}

func TestCgroupLeaseOwnerKindIsUnforgeableContextState(t *testing.T) {
	if got := cgroupLeaseOwnerKind(context.Background()); got != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD {
		t.Fatalf("ordinary owner kind = %s", got)
	}
	ctx := context.WithValue(context.Background(), internalConformanceContextKey{}, true)
	if got := cgroupLeaseOwnerKind(ctx); got != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
		t.Fatalf("internal conformance owner kind = %s", got)
	}
	if got := cgroupMemoryReservation(context.Background(), 123); got != 123 {
		t.Fatalf("workload reservation = %d", got)
	}
	if got := cgroupMemoryReservation(ctx, 0); got != config.RuntimeConformanceMemoryMaxBytes {
		t.Fatalf("conformance reservation = %d, want aggregate ceiling %d", got, config.RuntimeConformanceMemoryMaxBytes)
	}
}

func assertExactContainerExit(t *testing.T, fixture testAllocationController, containerID string, exitCode int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := fixture.manager.Get(containerID)
		if err == nil {
			status := c.Status.Get()
			if status.ExitCodeKnown && status.ExitCode == exitCode {
				if status.Message != "" {
					t.Fatalf("container exit message = %q, want empty for exact runtime exit", status.Message)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, err := fixture.manager.Get(containerID)
	if err != nil {
		t.Fatalf("Get(%q) after exit deadline: %v", containerID, err)
	}
	t.Fatalf("container status after exit deadline = %+v, want known exit code %d", c.Status.Get(), exitCode)
}
