package service

import (
	"testing"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestRequirementInputIncludesEgressPolicyCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		policy     *commonv1.NetworkEgressPolicy
		wantDNS    bool
		wantStrict bool
	}{
		{name: "none"},
		{
			name: "dns deny",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{
				DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"blocked.test"}},
			}},
			wantDNS: true,
		},
		{
			name: "strict domain",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"allowed.test"}},
			}},
			wantStrict: true,
		},
		{
			name: "strict cidr",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: []*commonv1.CIDREgressRule{{Cidr: "198.19.0.0/24"}}},
			}},
			wantStrict: true,
		},
		{
			name: "strict deny all",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{},
			}},
		},
	}

	service := &sandboxService{config: config.DefaultConfig()}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := service.requirementInput(&runtime.StartRequest{Network: &commonv1.NetworkSpec{EgressPolicy: test.policy}}, false)
			if input.RequiresDNSPolicyEnforcement != test.wantDNS || input.RequiresStrictEgressEnforcement != test.wantStrict {
				t.Fatalf("policy requirements = (dns=%t, strict=%t), want (dns=%t, strict=%t)",
					input.RequiresDNSPolicyEnforcement, input.RequiresStrictEgressEnforcement, test.wantDNS, test.wantStrict)
			}
		})
	}
}

func TestNetworkPolicyRequiresExactNodeDependencies(t *testing.T) {
	dns, err := axernsdk.DenyDNSNetworkPolicy("blocked.example")
	if err != nil {
		t.Fatal(err)
	}
	strict, err := axernsdk.NewStrictNetworkPolicy(nil, []axernsdk.CIDRRule{{
		CIDR: "198.19.0.2/32", Protocol: axernsdk.EgressProtocolTCP, Ports: []axernsdk.PortRange{{Start: 18080}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		policy   *axernsdk.NetworkPolicy
		required capabilityv1.PlatformCapability
		other    capabilityv1.PlatformCapability
	}{
		{"dns deny", dns, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT},
		{"strict cidr", strict, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.NatBackend = "bridge"
			service := &sandboxService{config: cfg}
			request := &runtime.StartRequest{
				AllocationID: "alloc-policy-contract",
				Environment:  &runtime.ResolvedEnvironment{Rootfs: &runtime.RootfsConfig{}},
				Network:      &commonv1.NetworkSpec{EgressPolicy: test.policy.Proto()},
				Resources:    &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 256 << 20, EphemeralStorageBytes: 128 << 20}},
			}
			for _, platform := range []capabilityv1.PlatformCapability{
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
				test.required,
			} {
				definition, ok := capabilitycontract.PlatformDefinition(platform)
				if !ok {
					t.Fatalf("missing platform definition %v", platform)
				}
				request.CapabilityRequirements = append(request.CapabilityRequirements, &capabilityv1.CapabilityRequirement{
					Key: capabilitycontract.PlatformKey(platform), LossPolicy: definition.LossPolicy,
				})
			}
			if err := service.verifyRequestCapabilityRequirements(request); err != nil {
				t.Fatalf("matching policy contract rejected: %v", err)
			}
			for _, mutation := range []string{"missing", "wrong policy", "extra", "duplicate", "removed policy"} {
				t.Run(mutation, func(t *testing.T) {
					changed := proto.Clone(request).(*runtime.StartRequest)
					switch mutation {
					case "missing":
						changed.CapabilityRequirements = changed.CapabilityRequirements[:3]
					case "wrong policy":
						changed.CapabilityRequirements[3].Key = capabilitycontract.PlatformKey(test.other)
					case "extra":
						changed.CapabilityRequirements = append(changed.CapabilityRequirements, &capabilityv1.CapabilityRequirement{Key: capabilitycontract.PlatformKey(test.other)})
					case "duplicate":
						changed.CapabilityRequirements = append(changed.CapabilityRequirements, proto.Clone(changed.CapabilityRequirements[3]).(*capabilityv1.CapabilityRequirement))
					case "removed policy":
						changed.Network.EgressPolicy = nil
					}
					if err := service.verifyRequestCapabilityRequirements(changed); err == nil {
						t.Fatal("mismatched policy/dependency contract accepted")
					}
				})
			}
		})
	}
}
