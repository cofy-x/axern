package pgrun

import (
	"context"
	"testing"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestNetworkPolicyAdmissionPreservesRequirementsAndRechecksCurrentNode(t *testing.T) {
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
	}{
		{"dns deny", dns, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT},
		{"strict cidr", strict, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newEnvironmentTestDB(t)
			ctx := context.Background()
			now := time.Now().UTC()
			engine := placement.NewEngine(placement.Config{})
			store := NewStore(db, WithPlacementEvaluator(engine))
			t.Cleanup(store.Close)
			env, err := store.CreateEnvironment(ctx, runkernel.CreateEnvironmentParams{
				Spec:         &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/python:3.12-slim"}},
				ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{},
			}, now)
			if err != nil {
				t.Fatal(err)
			}
			platforms := []capabilityv1.PlatformCapability{
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
				capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
				test.required,
			}
			summary := controldtest.ReadySummary(now)
			summary.CapabilitySnapshot = controldtest.AvailableCapabilitySnapshot(now, platforms...)
			const nodeID = "node-policy-admission"
			if _, err := db.Pool().Exec(ctx, `INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
				VALUES ($1, '127.0.0.1:24010', repeat('0', 64), $2, $2, 'active')`, nodeID, now); err != nil {
				t.Fatal(err)
			}
			publishSummary := func() {
				t.Helper()
				payload, err := protojson.Marshal(summary)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Pool().Exec(ctx, `INSERT INTO node_summaries (node_id, summary) VALUES ($1, $2::jsonb)
					ON CONFLICT (node_id) DO UPDATE SET summary = EXCLUDED.summary`, nodeID, payload); err != nil {
					t.Fatal(err)
				}
			}
			publishSummary()
			registry := nodekernel.NewRegistry()
			registry.Replace([]*nodekernel.Record{{NodeID: nodeID, NodeTarget: "127.0.0.1:24010", Lifecycle: nodekernel.LifecycleActive,
				AdmittedAt: now, LastHeartbeatAt: now, Summary: summary}})
			selector := placement.NewSelector(registry, engine, func() time.Time { return now })
			cfg, err := executionkernel.NormalizeConfigForRootfs(&commonv1.ExecutionConfig{
				Network:   &commonv1.NetworkSpec{EgressPolicy: test.policy.Proto()},
				Resources: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 256 << 20, EphemeralStorageBytes: 128 << 20}},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := selector.SelectCandidates(ctx, env, cfg)
			if err != nil {
				t.Fatal(err)
			}
			params := runkernel.AdmitRunParams{Namespace: "default", Environment: env, Config: cfg, Candidates: candidates}
			run, err := store.AdmitRun(ctx, params, now)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := store.GetRun(ctx, run.GetID())
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(stored.GetConfig().GetNetwork(), cfg.GetNetwork()) {
				t.Fatalf("stored policy = %v", stored.GetConfig().GetNetwork())
			}
			requirements, err := pgallocation.LoadCapabilityRequirements(ctx, db.Pool(), run.GetAllocationID())
			if err != nil {
				t.Fatal(err)
			}
			var got, want []*capabilityv1.CapabilityKey
			for _, requirement := range requirements {
				got = append(got, requirement.GetKey())
			}
			for _, platform := range platforms {
				want = append(want, capabilitycontract.PlatformKey(platform))
			}
			if !capabilitycontract.RequirementKeysEqual(got, want) {
				t.Fatalf("persisted requirements = %v, want %v", got, want)
			}
			// Keep the old selected candidates, but withdraw policy enforcement in
			// the authoritative Node row before the next admission transaction.
			summary.CapabilitySnapshot = controldtest.AvailableCapabilitySnapshot(now, platforms[:len(platforms)-1]...)
			publishSummary()
			if _, err := store.AdmitRun(ctx, params, now); err == nil {
				t.Fatal("stale selected policy capability bypassed the locked admission recheck")
			}
			var count int
			if err := db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM allocations WHERE node_id = $1`, nodeID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("rejected admission created an allocation: count=%d", count)
			}
		})
	}
}
