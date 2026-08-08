package placement

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func TestBuildRequestRequiresCgroupMemoryReadinessForHardLimit(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if !containsString(request.CapabilityRequirements, "cgroup:memory-limit-ready") {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}

	request = selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if containsString(request.CapabilityRequirements, "cgroup:memory-limit-ready") {
		t.Fatalf("memory request without a hard limit requires enforcement capability: %#v", request.CapabilityRequirements)
	}
}

func TestBuildRequestUsesEphemeralStorageContract(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: 512 << 20}},
	})
	if got, want := request.RequestedEphemeralStorageBytes, int64(512<<20); got != want {
		t.Fatalf("requested ephemeral storage = %d, want %d", got, want)
	}
	if !containsString(request.CapabilityRequirements, "runtime:runsc:ephemeral-storage-hard-limit") {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}
	if containsString(request.CapabilityRequirements, "runtime:runsc:writable-layer-hard-limit") {
		t.Fatalf("legacy writable-layer capability leaked into request: %#v", request.CapabilityRequirements)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
