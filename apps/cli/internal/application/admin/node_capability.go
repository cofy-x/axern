package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type NodeCapabilityCheck struct {
	keys          []*capabilityv1.CapabilityKey
	bootID        string
	observedAfter time.Time
}

type NodeCapabilityCheckResult struct {
	Ready  bool
	Reason string
}

func NewNodeCapabilityCheck(platforms []string, bootID, observedAfter string) (NodeCapabilityCheck, error) {
	if len(platforms) == 0 {
		return NodeCapabilityCheck{}, fmt.Errorf("at least one --platform is required")
	}
	if err := capabilitycontract.ValidateEvidence(capabilitycontract.BootEvidence(bootID), capabilitycontract.IdentityBoot); err != nil {
		return NodeCapabilityCheck{}, fmt.Errorf("--boot-id: %w", err)
	}
	var lowerBound time.Time
	if observedAfter != "" {
		var err error
		lowerBound, err = time.Parse(time.RFC3339Nano, observedAfter)
		if err != nil {
			return NodeCapabilityCheck{}, fmt.Errorf("--observed-after must be an RFC3339 timestamp: %w", err)
		}
	}
	keys := make([]*capabilityv1.CapabilityKey, 0, len(platforms))
	for _, name := range platforms {
		value, ok := capabilityv1.PlatformCapability_value["PLATFORM_CAPABILITY_"+name]
		if !ok {
			return NodeCapabilityCheck{}, fmt.Errorf("unknown platform capability %q; use its uppercase name without PLATFORM_CAPABILITY_", name)
		}
		keys = append(keys, capabilitycontract.PlatformKey(capabilityv1.PlatformCapability(value)))
	}
	if err := capabilitycontract.ValidateRequirementKeys(keys); err != nil {
		return NodeCapabilityCheck{}, err
	}
	return NodeCapabilityCheck{keys: keys, bootID: bootID, observedAfter: lowerBound}, nil
}

func (c NodeControl) CheckCapabilities(ctx context.Context, nodeID string, check NodeCapabilityCheck) (NodeCapabilityCheckResult, error) {
	nodeID = strings.TrimSpace(nodeID)
	snapshot, err := c.CapabilitySnapshot(ctx, nodeID)
	if err != nil {
		return NodeCapabilityCheckResult{}, err
	}
	nodes, err := c.List(ctx, "")
	if err != nil {
		return NodeCapabilityCheckResult{}, err
	}
	return check.evaluate(nodes.GetNodes(), snapshot.GetSnapshot(), nodeID, time.Now()), nil
}

func (c NodeCapabilityCheck) evaluate(nodes []*adminv1.AdminNode, snapshot *capabilityv1.CapabilitySnapshot, nodeID string, now time.Time) NodeCapabilityCheckResult {
	active := false
	for _, node := range nodes {
		if node.GetNodeID() == nodeID {
			active = node.GetLifecycleStatus() == adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE &&
				node.GetHeartbeatFresh() && node.GetSummaryFresh() && node.GetAxnodedReady()
			break
		}
	}
	if !active {
		return NodeCapabilityCheckResult{Reason: "node is not active with a fresh heartbeat, fresh summary, and ready runtime"}
	}
	if _, err := capabilitycontract.ResolveRequirements(snapshot, c.keys, now); err != nil {
		return NodeCapabilityCheckResult{Reason: "node capability evidence is invalid, expired, or unavailable"}
	}
	if !c.observedAfter.IsZero() && snapshot.GetCollectedAt().AsTime().Before(c.observedAfter) {
		return NodeCapabilityCheckResult{Reason: "node capability snapshot was collected before --observed-after"}
	}
	// ResolveRequirements owns evidence, expiry, and dependency validation. Bind
	// every host identity in that same dependency closure to the expected boot
	// and every observation to the optional process-start freshness boundary.
	visited := make(map[capabilityv1.PlatformCapability]bool)
	var checkEvidence func(*capabilityv1.CapabilityKey) error
	checkEvidence = func(key *capabilityv1.CapabilityKey) error {
		if visited[key.GetPlatform()] {
			return nil
		}
		visited[key.GetPlatform()] = true
		observation, available := capabilitycontract.AvailableObservation(snapshot, key, now)
		if !available {
			return fmt.Errorf("required capability %s is not available", key.GetPlatform())
		}
		if !c.observedAfter.IsZero() && !observation.GetObservedAt().AsTime().After(c.observedAfter) {
			return fmt.Errorf("required capability %s was not observed after %s", key.GetPlatform(), c.observedAfter.UTC().Format(time.RFC3339Nano))
		}
		evidence := observation.GetEvidence()
		var bootID string
		switch identity := evidence.GetIdentity().(type) {
		case *capabilityv1.CapabilityEvidence_Boot:
			bootID = identity.Boot.GetBootID()
		case *capabilityv1.CapabilityEvidence_Mount:
			bootID = identity.Mount.GetBootID()
		case *capabilityv1.CapabilityEvidence_Runtime:
			bootID = identity.Runtime.GetBootID()
		}
		if bootID != "" && bootID != c.bootID {
			return fmt.Errorf("required capability %s has evidence from a different boot", key.GetPlatform())
		}
		dependencies, err := capabilitycontract.PlatformDependencyKeys(key.GetPlatform())
		if err != nil {
			return err
		}
		for _, dependency := range dependencies {
			if err := checkEvidence(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	for _, key := range c.keys {
		if err := checkEvidence(key); err != nil {
			return NodeCapabilityCheckResult{Reason: err.Error()}
		}
	}
	return NodeCapabilityCheckResult{Ready: true}
}
