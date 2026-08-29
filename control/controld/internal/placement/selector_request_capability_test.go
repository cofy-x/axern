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
	request, err := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}

	request, err = selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 1 << 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT) {
		t.Fatalf("memory request without a hard limit requires enforcement capability: %#v", request.CapabilityRequirements)
	}
}

func TestBuildRequestUsesEphemeralStorageContract(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request, err := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: 512 << 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := request.RequestedEphemeralStorageBytes, int64(512<<20); got != want {
		t.Fatalf("requested ephemeral storage = %d, want %d", got, want)
	}
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}
}

func TestBuildRequestPersistsNetworkAndPortDependencies(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	request, err := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{
		Ports:   []*commonv1.PortSpec{{ContainerPort: 8080}},
		Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING) {
		t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
	}
	now := time.Now().UTC()
	candidate, err := requestForCandidate(request, record("node-a", []string{"runsc"}, readySummary(now), now), now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPlatform(candidate.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE) {
		t.Fatalf("candidate capabilities = %#v", candidate.CapabilityRequirements)
	}
}

func TestBuildRequestDerivesNetworkPolicyCapabilities(t *testing.T) {
	selector := &Selector{defaultSandboxRuntime: "runsc"}
	tests := []struct {
		name      string
		network   *commonv1.NetworkSpec
		required  capabilityv1.PlatformCapability
		forbidden capabilityv1.PlatformCapability
	}{
		{
			name:      "dns deny",
			network:   &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"example.com"}}}}},
			required:  capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT,
			forbidden: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT,
		},
		{
			name:      "strict allow",
			network:   &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"example.com"}}}}},
			required:  capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT,
			forbidden: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{Network: tt.network})
			if err != nil {
				t.Fatal(err)
			}
			if !containsPlatform(request.CapabilityRequirements, tt.required) || containsPlatform(request.CapabilityRequirements, tt.forbidden) {
				t.Fatalf("capabilities = %#v", request.CapabilityRequirements)
			}
		})
	}

	request, err := selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if containsPlatform(request.CapabilityRequirements, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT) {
		t.Fatalf("isolated deny-all unexpectedly requires egressd: %#v", request.CapabilityRequirements)
	}

	_, err = selector.buildRequest(&environmentv1.Environment{ID: "env-a"}, &commonv1.ExecutionConfig{Network: &commonv1.NetworkSpec{
		Mode:         commonv1.NetworkMode_NETWORK_MODE_HOST,
		EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}},
	}})
	if err == nil {
		t.Fatal("host networking with a policy was accepted by placement")
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
