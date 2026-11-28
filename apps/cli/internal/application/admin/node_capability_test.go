package admin

import (
	"context"
	"fmt"
	"slices"
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
		reason string
	}{
		{"expired bridge with fresh summary", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			observation := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
			observation.ObservedAt = timestamppb.New(now.Add(-16 * time.Second))
			observation.ValidUntil = timestamppb.New(now.Add(-time.Second))
		}, "NETWORK_BRIDGE has expired"},
		{"expired dependency of available storage", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			observation := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
			observation.ObservedAt = timestamppb.New(now.Add(-15 * time.Second))
			observation.ValidUntil = timestamppb.New(now)
		}, "FILESTORE_OVERLAYFS_UPPER has expired"},
		{"missing dependency of available DNS", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			snapshot.Observations = slices.DeleteFunc(snapshot.Observations, func(observation *capabilityv1.CapabilityObservation) bool {
				return observation.GetKey().GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST
			})
		}, "EGRESSD_DNS_POLICY_SELF_TEST is missing"},
		{"wrong provider", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE).Provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
		}, "node capability snapshot has an invalid observation for NETWORK_BRIDGE"},
		{"missing expiry", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE).ValidUntil = nil
		}, "node capability snapshot has an invalid observation for NETWORK_BRIDGE"},
		{"invalid runtime digest", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST).Evidence.GetRuntime().RuntimeBinaryDigest = "not-a-digest"
		}, "node capability snapshot has an invalid observation for RUNSC_MEMORY_ENFORCEMENT_SELF_TEST"},
		{"duplicate observation", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			snapshot.Observations = append(snapshot.Observations, snapshot.Observations[0])
		}, "node capability snapshot has invalid or duplicate observation keys"},
		{"future publication", func(_ []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot) {
			snapshot.CollectedAt = timestamppb.New(now.Add(2 * time.Minute))
		}, "node capability snapshot collection time is in the future"},
		{"stale summary", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].SummaryFresh = false }, "node is not active"},
		{"stale heartbeat", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].HeartbeatFresh = false }, "node is not active"},
		{"runtime not ready", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) { nodes[0].AxnodedReady = false }, "node is not active"},
		{"revoked node", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) {
			nodes[0].LifecycleStatus = adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_REVOKED
		}, "node is not active"},
		{"different node", func(nodes []*adminv1.AdminNode, _ *capabilityv1.CapabilitySnapshot) {
			nodes[0].NodeID = "different-node"
		}, "node is not active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check, nodes, snapshot := capabilityCheckFixture(t, now)
			if result := check.evaluate(nodes, snapshot, "node-test", now); !result.Ready {
				t.Fatalf("valid fixture failed: %s", result.Reason)
			}
			tc.change(nodes, snapshot)
			if result := check.evaluate(nodes, snapshot, "node-test", now); result.Ready || !strings.Contains(result.Reason, tc.reason) {
				t.Fatalf("invalid evidence = %+v, want rejection mentioning %q", result, tc.reason)
			}
		})
	}
}

func TestNodeCapabilityCheckReportsBoundedResolutionFailures(t *testing.T) {
	now := time.Date(2026, 11, 28, 2, 40, 0, 0, time.UTC)
	const privateReason = "node-provided text with private runtime context"
	for _, tc := range []struct {
		name   string
		change func(*capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot
		want   string
	}{
		{"missing snapshot", func(*capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
			return nil
		}, "node capability snapshot is missing"},
		{"missing requirement", func(snapshot *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
			snapshot.Observations = slices.DeleteFunc(snapshot.Observations, func(observation *capabilityv1.CapabilityObservation) bool {
				return observation.GetKey().GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE
			})
			return snapshot
		}, "NETWORK_BRIDGE is missing"},
		{"published expiry", func(snapshot *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
			observation := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
			observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
			observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED
			observation.Reason = privateReason
			return snapshot
		}, "NETWORK_BRIDGE has expired"},
		{"failed memory self-test", func(snapshot *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
			requirement := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT)
			requirement.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
			requirement.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE
			requirement.Reason = privateReason
			selfTest := checkObservation(snapshot, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST)
			selfTest.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
			selfTest.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
			selfTest.Reason = privateReason
			return snapshot
		}, "RUNSC_MEMORY_HARD_LIMIT has an unavailable dependency. RUNSC_MEMORY_ENFORCEMENT_SELF_TEST is unavailable (PROBE_FAILED)"},
		{"invalid extension key", func(snapshot *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
			snapshot.Observations = append(snapshot.Observations, &capabilityv1.CapabilityObservation{
				Key: capabilitycontract.ExtensionKey(privateReason, "private extension value"),
			})
			return snapshot
		}, "node capability snapshot has invalid or duplicate observation keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check, nodes, snapshot := capabilityCheckFixture(t, now)
			snapshot = tc.change(snapshot)
			if _, err := capabilitycontract.ResolveRequirements(snapshot, check.keys, now); err == nil {
				t.Fatal("invalid evidence passed the shared admission checker")
			}
			result := check.evaluate(nodes, snapshot, "node-test", now)
			if result.Ready || result.Reason != tc.want {
				t.Fatalf("readiness = %+v, want rejection with reason %q", result, tc.want)
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
		if _, err := NewNodeCapabilityCheck(platforms, capabilityCheckBootID, ""); err == nil {
			t.Fatalf("accepted invalid platform requirements %v", platforms)
		}
	}
	for _, bootID := range []string{"", "current", strings.ToUpper(previousCapabilityBootID)} {
		if _, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE"}, bootID, ""); err == nil {
			t.Fatalf("accepted invalid boot ID %q", bootID)
		}
	}
}

func TestNodeCapabilityCheckRejectsPreRestartSnapshotFromSameBoot(t *testing.T) {
	now := time.Date(2026, 11, 6, 1, 0, 2, 0, time.UTC)
	check, nodes, snapshot := capabilityCheckFixture(t, now.Add(-2*time.Second))
	if result := check.evaluate(nodes, snapshot, "node-test", now); !result.Ready {
		t.Fatalf("same-boot evidence should remain valid without a restart fence: %+v", result)
	}
	fenced, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE"}, capabilityCheckBootID, now.Add(-time.Second).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if result := fenced.evaluate(nodes, snapshot, "node-test", now); result.Ready || !strings.Contains(result.Reason, "snapshot was collected before") {
		t.Fatalf("previous-process snapshot passed restart fence: %+v", result)
	}
}

func TestNodeCapabilityCheckRequiresEveryDependencyAfterFence(t *testing.T) {
	now := time.Date(2026, 11, 6, 1, 0, 2, 987654321, time.UTC)
	lower := now.Add(-time.Second)
	for _, platform := range []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST,
	} {
		for _, observedAt := range []time.Time{lower.Add(-time.Nanosecond), lower} {
			t.Run(platform.String()+"/"+observedAt.Format(time.RFC3339Nano), func(t *testing.T) {
				check, nodes, snapshot := capabilityCheckFixture(t, now)
				fenced, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE", "RUNSC_MEMORY_HARD_LIMIT", "RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT"}, capabilityCheckBootID, lower.Format(time.RFC3339Nano))
				if err != nil {
					t.Fatal(err)
				}
				if result := fenced.evaluate(nodes, snapshot, "node-test", now); !result.Ready {
					t.Fatalf("post-restart observations rejected: %+v", result)
				}
				observation := checkObservation(snapshot, platform)
				observation.ObservedAt = timestamppb.New(observedAt)
				if observation.ValidUntil != nil {
					observation.ValidUntil = timestamppb.New(observation.ValidUntil.AsTime().Add(observedAt.Sub(now)))
				}
				if result := check.evaluate(nodes, snapshot, "node-test", now); !result.Ready {
					t.Fatalf("fixture must still meet ordinary admission freshness: %+v", result)
				}
				result := fenced.evaluate(nodes, snapshot, "node-test", now)
				if result.Ready || !strings.Contains(result.Reason, platform.String()) || !strings.Contains(result.Reason, "was not observed after") {
					t.Fatalf("old dependency accepted behind a newly published capability: %+v", result)
				}
			})
		}
	}
}

func TestNodeCapabilityCheckValidatesObservationLowerBound(t *testing.T) {
	for _, value := range []string{"yesterday", "2026-11-06", "2026-11-06T12:00:00", "2026-13-06T12:00:00Z", " "} {
		if _, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE"}, capabilityCheckBootID, value); err == nil || !strings.Contains(err.Error(), "--observed-after") {
			t.Fatalf("accepted invalid lower bound %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "2026-11-06T12:00:00Z", "2026-11-06T20:00:00.123456789+08:00"} {
		if _, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE"}, capabilityCheckBootID, value); err != nil {
			t.Fatalf("valid lower bound %q: %v", value, err)
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
	check, err := NewNodeCapabilityCheck([]string{"NETWORK_BRIDGE", "DNS_POLICY_ENFORCEMENT", "STRICT_EGRESS_ENFORCEMENT", "RUNSC_MEMORY_HARD_LIMIT", "RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT"}, capabilityCheckBootID, "")
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
