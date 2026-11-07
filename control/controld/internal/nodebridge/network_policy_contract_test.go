package nodebridge

import (
	"context"
	"testing"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Follow a public SDK policy through normalization, the persisted Run config
// encoding, candidate selection, the locked-admission re-resolution, and the
// node lifecycle wire. The selected node must receive both the exact policy
// and the matching immutable requirements, including after a second resolve.
func TestNetworkPolicyCapabilityDeliveryContract(t *testing.T) {
	dns, err := axernsdk.DenyDNSNetworkPolicy("Blocked.Example.")
	if err != nil {
		t.Fatal(err)
	}
	strict, err := axernsdk.NewStrictNetworkPolicy(nil, []axernsdk.CIDRRule{{
		CIDR: "198.19.0.2/32", Protocol: axernsdk.EgressProtocolTCP,
		Ports: []axernsdk.PortRange{{Start: 18080}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	domain, err := axernsdk.AllowDomainNetworkPolicy("Allowed.Example.")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		policy *axernsdk.NetworkPolicy
		want   capabilityv1.PlatformCapability
	}{
		{name: "unrestricted"},
		{name: "dns deny", policy: dns, want: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT},
		{name: "strict cidr", policy: strict, want: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT},
		{name: "strict domain", policy: domain, want: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT},
		{name: "deny all", policy: axernsdk.DenyAllNetworkPolicy()},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			cfg, err := executionkernel.NormalizeConfigForRootfs(&commonv1.ExecutionConfig{
				Network: &commonv1.NetworkSpec{EgressPolicy: test.policy.Proto()},
				Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{
					MemoryBytes: 256 << 20, EphemeralStorageBytes: 128 << 20,
				}},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := protojson.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			stored := &commonv1.ExecutionConfig{}
			if err := protojson.Unmarshal(payload, stored); err != nil {
				t.Fatal(err)
			}
			summary := controldtest.ReadySummary(now)
			platforms := []capabilityv1.PlatformCapability{
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
			}
			if test.want != capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED {
				platforms = append(platforms, test.want)
			}
			summary.CapabilitySnapshot = controldtest.AvailableCapabilitySnapshot(now, platforms...)
			record := &nodekernel.Record{NodeID: "node-network", NodeTarget: "node:24010", Lifecycle: nodekernel.LifecycleActive,
				AdmittedAt: now, LastHeartbeatAt: now, Summary: summary}
			registry := nodekernel.NewRegistry()
			registry.Replace([]*nodekernel.Record{record})
			selector := placement.NewSelector(registry, placement.NewEngine(placement.Config{}), func() time.Time { return now })
			env := &environmentv1.Environment{ID: "env-network", ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{}}
			candidates, err := selector.SelectCandidates(context.Background(), env, stored)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("select candidate: candidates=%v error=%v", candidates, err)
			}
			wantKeys := make([]*capabilityv1.CapabilityKey, 0, len(platforms))
			for _, platform := range platforms {
				wantKeys = append(wantKeys, capabilitycontract.PlatformKey(platform))
			}
			if !capabilitycontract.RequirementKeysEqual(candidates[0].Request.CapabilityRequirements, wantKeys) {
				t.Fatalf("candidate requirements = %v, want %v", candidates[0].Request.CapabilityRequirements, wantKeys)
			}
			// This is the same entry point used after locking the current Node row.
			locked, err := placementkernel.ResolveRequestForNode(candidates[0].BaseRequest, summary, now)
			if err != nil {
				t.Fatal(err)
			}
			requirements, err := capabilitycontract.ResolveRequirements(summary.CapabilitySnapshot, locked.CapabilityRequirements, now)
			if err != nil {
				t.Fatal(err)
			}
			request := buildCreateAllocationRequestFromParams(createAllocationRequestParams{
				AllocationID: "alloc-network", NodeID: record.NodeID, Environment: env,
				Config: stored, CapabilityRequirements: requirements,
			})
			wire, err := proto.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			delivered := &privatenodev1.CreateAllocationRequest{}
			if err := proto.Unmarshal(wire, delivered); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(delivered.GetConfig().GetNetwork(), cfg.GetNetwork()) {
				t.Fatalf("policy lost between public config and node: %v", delivered.GetConfig().GetNetwork())
			}
			var deliveredKeys []*capabilityv1.CapabilityKey
			for _, requirement := range delivered.GetConfig().GetCapabilityRequirements() {
				deliveredKeys = append(deliveredKeys, requirement.GetKey())
			}
			if !capabilitycontract.RequirementKeysEqual(deliveredKeys, wantKeys) {
				t.Fatalf("delivered requirements = %v, want %v", deliveredKeys, wantKeys)
			}
			if test.want == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED {
				return
			}
			// Losing egress support after selection must fail the admission gate,
			// even though ordinary bridge/memory/storage remain healthy.
			summary.CapabilitySnapshot = controldtest.AvailableCapabilitySnapshot(now, platforms[:len(platforms)-1]...)
			locked, err = placementkernel.ResolveRequestForNode(candidates[0].BaseRequest, summary, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := capabilitycontract.ResolveRequirements(summary.CapabilitySnapshot, locked.CapabilityRequirements, now); err == nil {
				t.Fatal("missing current policy capability was admitted after selection")
			}
			registry.Replace([]*nodekernel.Record{record})
			if _, err := selector.SelectCandidates(context.Background(), env, stored); err == nil {
				t.Fatal("node without policy enforcement was eligible")
			}
		})
	}
}
