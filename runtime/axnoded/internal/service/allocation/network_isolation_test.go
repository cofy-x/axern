package allocation

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestIsolatedRunDoesNotAllocateAConnectedInterface(t *testing.T) {
	required := []resources.ResourceName{resources.CgroupResourceName, resources.InterfaceResourceName}
	isolated := &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED}
	got := allocationHostResources(required, isolated)
	if len(got) != 1 || got[0] != resources.CgroupResourceName {
		t.Fatalf("isolated resources = %v, want only cgroup", got)
	}
	for _, network := range []*commonv1.NetworkSpec{nil, {Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT}} {
		got = allocationHostResources(required, network)
		if len(got) != 2 || got[1] != resources.InterfaceResourceName {
			t.Fatalf("connected resources = %v, want cgroup and interface", got)
		}
	}
	if len(required) != 2 {
		t.Fatalf("runtime host requirements were mutated: %v", required)
	}
}

func TestIsolatedRunRejectsAccidentalNetworkBinding(t *testing.T) {
	controller := &Controller{}
	_, err := controller.createHandlerOptions("", "", nil, nil, container.OccupiedResource{
		ID: "alloc-isolated", Resources: map[resources.ResourceName]string{
			resources.InterfaceResourceName: "unexpected-veth",
		},
	}, &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED}, nil)
	if err == nil || !strings.Contains(err.Error(), "connected network binding") {
		t.Fatalf("connected isolated allocation was not rejected: %v", err)
	}
}

func TestIsolatedRunSelectsLoopbackOnlyRuntimeNetwork(t *testing.T) {
	controller := &Controller{}
	resource := container.OccupiedResource{
		ID: "alloc-isolated",
		Resources: map[resources.ResourceName]string{
			resources.CgroupResourceName: "/sys/fs/cgroup/alloc-isolated",
		},
	}
	for _, network := range []*commonv1.NetworkSpec{
		{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED},
		{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}},
	} {
		options, err := controller.createHandlerOptions("", "", nil, nil, resource, network, nil)
		if err != nil {
			t.Fatalf("createHandlerOptions(%v) error = %v", network, err)
		}
		if options.NetworkMode != commonv1.NetworkMode_NETWORK_MODE_ISOLATED || options.NetworkNamespacePath != "" {
			t.Fatalf("isolated runtime options = mode %s, namespace %q", options.NetworkMode, options.NetworkNamespacePath)
		}
	}
	connected, err := controller.createHandlerOptions("", "", nil, nil, resource, nil, nil)
	if err != nil {
		t.Fatalf("createHandlerOptions(default) error = %v", err)
	}
	if connected.NetworkMode == commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		t.Fatal("default network accidentally selected runsc loopback-only mode")
	}
}
