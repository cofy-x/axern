package placement

import (
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func TestBuildRequestRequiresCgroupMemoryReadinessForHardLimit(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}

	request = selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT) {
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
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}
}

func TestBuildRequestPersistsNetworkAndPortDependencies(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Ports:   []*commonv1.PortSpec{{ContainerPort: 8080}},
		Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT},
	})
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}
	now := time.Now().UTC()
	candidate := requestForCandidate(request, record("node-a", []string{"runsc"}, readySummary(now), now), now)
	if !containsPlatform(candidate.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE) {
		t.Fatalf("candidate capabilities = %#v", candidate.CapabilityRequirements)
	}
}

func containsPlatform(values []*capabilityv1.CapabilityKey, want capabilityv1.PlatformCapability) bool {
	wantID, _ := capabilitycontract.KeyID(capabilitycontract.PlatformKey(want))
	for _, value := range values {
		id, _ := capabilitycontract.KeyID(value)
		if id == wantID {
			return true
		}
	}
	return false
}
