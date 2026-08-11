package allocation

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestCreateRuntimeContainerSyncsRuntimeStateIntoStatus(t *testing.T) {
	handler := &runtimeSpyHandler{
		name: "runc",
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
}
