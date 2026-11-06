package admin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const capabilityCheckBootID = "11111111-2222-3333-4444-555555555555"
const previousCapabilityBootID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestNodeCapabilityCheckUsesAdmissionValidity(t *testing.T) {
	now := time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func([]*adminv1.AdminNode, *capabilityv1.CapabilitySnapshot)
	}{
		{"expired bridge with fresh summary", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			observation := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
			observation.ObservedAt = timestamppb.New(now.Add(-16 * time.Second))
			observation.ValidUntil = timestamppb.New(now.Add(-time.Second))
		}},
		{"expired dependency of available storage", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			observation := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
			observation.ObservedAt = timestamppb.New(now.Add(-15 * time.Second))
			observation.ValidUntil = timestamppb.New(now)
		}},
		{"missing dependency of available DNS", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			for i, observation := range snapshot.Observations {
				if observation.Key.GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST {
					snapshot.Observations = append(snapshot.Observations[:i], snapshot.Observations[i+1:]...)
					break
				}
			}
		}},
		{"wrong provider", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE).Provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
		}},
		{"missing expiry", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE).ValidUntil = nil
		}},
		{"invalid runtime digest", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST).Evidence.GetRuntime().RuntimeBinaryDigest = "not-a-digest"
		}},
		{"duplicate observation", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			snapshot.Observations = append(snapshot.Observations, snapshot.Observations[0])
		}},
		{"future publication", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			snapshot.CollectedAt = timestamppb.New(now.Add(2 * time.Minute))
		}},
		{"stale summary", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].SummaryFresh = false }},
		{"stale heartbeat", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].HeartbeatFresh = false }},
		{"runtime not ready", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].AxnodedReady = false }},
		{"revoked node", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) {
			nodes[0].LifecycleStatus = adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_REVOKED
		}},
		{"different node", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) {
			nodes[0].NodeID = "different-node"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check, nodes, snapshot := capabilityCheckFixture(t, now)
			if result := check.evaluate(nodes, snapshot, "node-test", now); !result.Ready {
				t.Fatalf("valid fixture failed: %s", result.Reason)
			}
			tc.change(nodes, snapshot)
			if result := check.evaluate(nodes, snapshot, "node-test", now); result.Ready || result.Reason == "" {
				t.Fatalf("invalid evidence accepted: %+v", result)
			}
		})
	}
}

func TestNodeCapabilityCheckBindsEveryRequiredHostIdentityToBoot(t *testing.T) {
	now := time.Now()
	for _, key := range []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST,
	} {
		t.Run(key.String(), func(t *testing.T) {
			check, nodes, snapshot := capabilityCheckFixture(t, now)
			evidence := checkObservation(snapshot, key).Evidence
			switch identity := evidence.GetIdentity().(type) {
			case *capabilityv1.CapabilityEvidence_Boot:
				identity.Boot.BootID = previousCapabilityBootID
			case *capabilityv1.CapabilityEvidence_Mount:
				identity.Mount.BootID = previousCapabilityBootID
			case *capabilityv1.CapabilityEvidence_Runtime:
				identity.Runtime.BootID = previousCapabilityBootID
			}
			// These facts are structurally valid; expected-boot fencing is a
			// separate requirement from the canonical admission predicate.
			if _, err := capabilitycontract.ResolveRequirements(snapshot, check.keys, now); err != nil {
				t.Fatalf("unexpected canonical validation error: %v", err)
			}
			if result := check.evaluate(nodes, snapshot, "node-test", now); result.Ready || !strings.Contains(result.Reason, "different boot") {
				t.Fatalf("previous boot accepted: %+v", result)
			}
		})
	}
}

func TestNodeCapabilityCheckRejectsInvalidRequirements(t *testing.T) {
	for _, platforms := range [][]string{nil, {"UNKNOWN"}, {"UNSPECIFIED"}, {"NETWORK_BRIDGE", "NETWORK_BRIDGE"}, {"RUNSC_MEMORY_ENFORCEMENT_SELF_TEST"}} {
		if _, err := NewNodeCapabilityCheck(platforms, capabilityCheckBootID); err == nil {
			t.Fatalf("accepted invalid platform requirements %v", platforms)
		}
	}
	for _, bootID := range []string{"", "current", strings.ToUpper(previousCapabilityBootID)} {
		if _, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE"}, bootID); err == nil {
			t.Fatalf("accepted invalid boot ID %q", bootID)
		}
	}
}

type capabilityCheckClient struct {
	NodeClient
	nodes       []*adminv1.AdminNode
	snapshot    *capabilityv1.CapabilitySnapshot
	snapshotErr error
	listErr     error
	requestedID string
}

func (c *capabilityCheckClient) ListAdminNodes(context.Context, *adminv1.ListAdminNodesRequest, ...grpc.CallOption) (*adminv1.ListAdminNodesResponse, error) {
	return &adminv1.ListAdminNodesResponse{Nodes: c.nodes}, c.listErr
}

func (c *capabilityCheckClient) GetNodeCapabilitySnapshot(_ context.Context, request *adminv1.GetNodeCapabilitySnapshotRequest, _ ...grpc.CallOption) (*adminv1.GetNodeCapabilitySnapshotResponse, error) {
	c.requestedID = request.GetNodeID()
	return &adminv1.GetNodeCapabilitySnapshotResponse{Snapshot: c.snapshot}, c.snapshotErr
}

func TestNodeCapabilityCheckPreservesAPIErrors(t *testing.T) {
	check, nodes, snapshot := capabilityCheckFixture(t, time.Now())
	client := &capabilityCheckClient{nodes: nodes, snapshot: snapshot}
	result, err := NewNode(client).CheckCapabilities(context.Background(), " node-test ", check)
	if err != nil || !result.Ready || client.requestedID != "node-test" {
		t.Fatalf("check = %+v, %v; requested node = %q", result, err, client.requestedID)
	}
	for _, code := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.Unavailable} {
		for _, operation := range []string{"snapshot", "list"} {
			t.Run(fmt.Sprintf("%s/%s", operation, code), func(t *testing.T) {
				client := &capabilityCheckClient{nodes: nodes, snapshot: snapshot}
				if operation == "snapshot" {
					client.snapshotErr = status.Error(code, "test failure")
				} else {
					client.listErr = status.Error(code, "test failure")
				}
				if _, err := NewNode(client).CheckCapabilities(context.Background(), "node-test", check); status.Code(err) != code {
					t.Fatalf("lost API error classification: %v", err)
				}
			})
		}
	}
}

func capabilityCheckFixture(t *testing.T, now time.Time) (NodeCapabilityCheck, []*adminv1.AdminNode, *capabilityv1.CapabilitySnapshot) {
	t.Helper()
	check, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE", "DNS_POLICY_ENFORCEMENT", "STRICT_EGRESS_ENFORCEMENT", "RUNSC_MEMORY_HARD_LIMIT", "RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT"}, capabilityCheckBootID)
	if err != nil {
		t.Fatal(err)
	}
	nodes := []*adminv1.AdminNode{{NodeID: "node-test", LifecycleStatus: adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE, HeartbeatFresh: true, SummaryFresh: true, AxnodedReady: true}}
	snapshot := &capabilityv1.CapabilitySnapshot{CollectedAt: timestamppb.New(now)}
	seen := make(map[capabilityv1.PlatformCapability]bool)
	var add func(*capabilityv1.CapabilityKey)
	add = func(key *capabilityv1.CapabilityKey) {
		if seen[key.GetPlatform()] {
			return
		}
		seen[key.GetPlatform()] = true
		definition, _ := capabilitycontract.PlatformDefinition(key.GetPlatform())
		observation := &capabilityv1.CapabilityObservation{Key: key, Provider: definition.Provider, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, ObservedAt: timestamppb.New(now)}
		switch definition.Identity {
		case capabilitycontract.IdentityBoot:
			observation.Evidence = capabilitycontract.BootEvidence(capabilityCheckBootID)
		case capabilitycontract.IdentityMount:
			observation.Evidence = capabilitycontract.MountEvidence(capabilityCheckBootID, "42:/var/lib/axern:ext4")
		case capabilitycontract.IdentityRuntime:
			observation.Evidence = capabilitycontract.RuntimeEvidence(capabilityCheckBootID, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64))
		}
		if definition.Freshness.MaxValidity > 0 {
			observation.ValidUntil = timestamppb.New(now.Add(definition.Freshness.MaxValidity))
		}
		snapshot.Observations = append(snapshot.Observations, observation)
		for _, dependency := range definition.Dependencies {
			add(capabilitycontract.PlatformKey(dependency))
		}
	}
	for _, key := range check.keys {
		add(key)
	}
	return check, nodes, snapshot
}

func checkObservation(snapshot *capabilityv1.CapabilitySnapshot, platform capabilityv1.PlatformCapability) *capabilityv1.CapabilityObservation {
	for _, observation := range snapshot.Observations {
		if observation.GetKey().GetPlatform() == platform {
			return observation
		}
	}
	panic("missing fixture observation: " + platform.String())
}
