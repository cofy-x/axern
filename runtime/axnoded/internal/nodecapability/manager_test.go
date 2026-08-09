package nodecapability

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type testProvider struct {
	provider capabilityv1.CapabilityProvider
	keys     []*capabilityv1.CapabilityKey
	observe  func(time.Time) ([]*capabilityv1.CapabilityObservation, error)
}

func (p testProvider) Provider() capabilityv1.CapabilityProvider   { return p.provider }
func (p testProvider) ExpectedKeys() []*capabilityv1.CapabilityKey { return p.keys }
func (p testProvider) Observe(_ context.Context, now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
	return p.observe(now)
}

func TestManagerRejectsDuplicateOwnership(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	p := func(provider capabilityv1.CapabilityProvider) testProvider {
		return testProvider{provider: provider, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) { return nil, nil }}
	}
	if _, err := NewManager(p(capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG), p(capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH)); err == nil {
		t.Fatal("NewManager() accepted duplicate capability ownership")
	}
}

func TestManagerPublishesAtomicUnknownForMissingObservation(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return nil, errors.New("probe unavailable")
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshot, refreshErr := manager.Refresh(context.Background(), now)
	if refreshErr != nil || !manager.Ready() || snapshot.GetSequence() != 1 || len(snapshot.GetObservations()) != 1 {
		t.Fatalf("snapshot=%#v error=%v ready=%v", snapshot, refreshErr, manager.Ready())
	}
	observation := snapshot.GetObservations()[0]
	if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN || observation.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR {
		t.Fatalf("provider failure observation = %#v", observation)
	}
	if snapshot.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN {
		t.Fatalf("observation = %#v", snapshot.GetObservations()[0])
	}
}

func TestManagerSerializesRefreshGenerations(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	var active, maximum atomic.Int32
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return []*capabilityv1.CapabilityObservation{{
			Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE,
			ObservedAt:    timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(time.Minute)),
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Evidence:   &capabilityv1.CapabilityEvidence{ConfigDigest: "network"},
		}}, nil
	}}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.Refresh(context.Background(), time.Now().UTC()); err != nil {
				t.Errorf("Refresh() error = %v", err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("concurrent provider observations = %d, want 1", maximum.Load())
	}
	if snapshot := manager.Snapshot(); snapshot.GetSequence() != 2 {
		t.Fatalf("snapshot sequence = %d, want 2", snapshot.GetSequence())
	}
}

func TestManagerRejectsStaleRefreshTime(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return []*capabilityv1.CapabilityObservation{{Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN, ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, ObservedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR, Evidence: &capabilityv1.CapabilityEvidence{ConfigDigest: "network"}}}, nil
	}}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now.Add(-time.Second)); err == nil {
		t.Fatal("Refresh() accepted a timestamp older than the published snapshot")
	}
	if snapshot := manager.Snapshot(); snapshot.GetSequence() != 1 {
		t.Fatalf("snapshot sequence = %d, want stale refresh to leave it at 1", snapshot.GetSequence())
	}
}

func TestEvidenceIdentityDescribesFactsNotHealthState(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	base := &capabilityv1.CapabilityObservation{
		Key: key, Provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE,
		Evidence:      &capabilityv1.CapabilityEvidence{ConfigDigest: "network-v1"},
	}
	available := proto.Clone(base).(*capabilityv1.CapabilityObservation)
	available.State = capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	available.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
	failed := proto.Clone(base).(*capabilityv1.CapabilityObservation)
	failed.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	failed.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	failed.Reason = "dataplane unavailable"
	if evidenceID(available) != evidenceID(failed) {
		t.Fatal("health state changed the identity of otherwise identical evidence")
	}
	changed := proto.Clone(base).(*capabilityv1.CapabilityObservation)
	changed.Evidence.ConfigDigest = "network-v2"
	if evidenceID(available) == evidenceID(changed) {
		t.Fatal("evidence fact change did not change its identity")
	}
}

func TestManagerRejectsObservationWithWrongCatalogScope(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return []*capabilityv1.CapabilityObservation{{
			Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC,
			ObservedAt:    timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Evidence: &capabilityv1.CapabilityEvidence{ConfigDigest: "network"},
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("Refresh() accepted an observation with the wrong catalog scope")
	}
}

func TestManagerRecoveryRequiresTwoDistinctSuccessfulProbes(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	state := capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	observedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		reasonCode := capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
		if state == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			reasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
		}
		return []*capabilityv1.CapabilityObservation{{Key: key, State: state, ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, ObservedAt: timestamppb.New(observedAt), ValidUntil: timestamppb.New(observedAt.Add(time.Minute)), ReasonCode: reasonCode, Evidence: &capabilityv1.CapabilityEvidence{ConfigDigest: "network"}}}, nil
	}}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), observedAt); err != nil {
		t.Fatal(err)
	}
	state = capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	first, _ := manager.Refresh(context.Background(), observedAt.Add(5*time.Second))
	if first.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatalf("first recovery = %s", first.GetObservations()[0].GetState())
	}
	second, _ := manager.Refresh(context.Background(), observedAt.Add(10*time.Second))
	if second.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatal("cached success incorrectly completed recovery")
	}
	observedAt = observedAt.Add(10 * time.Second)
	third, _ := manager.Refresh(context.Background(), observedAt.Add(5*time.Second))
	if third.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatalf("second probe recovery = %s", third.GetObservations()[0].GetState())
	}
}

func TestManagerAdmitsCurrentDependencyEvidence(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	derivedProvider := testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED,
		keys:     []*capabilityv1.CapabilityKey{key},
		observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{
				Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
				ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE,
				ObservedAt:    timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(time.Minute)),
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
				Evidence:   &capabilityv1.CapabilityEvidence{EvidenceID: "current", ConfigDigest: "derived-v1"},
				Dependencies: []*capabilityv1.CapabilityEvidenceReference{
					{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER), EvidenceID: "mount-v2", Evidence: &capabilityv1.CapabilityEvidence{EvidenceID: "mount-v2", BootID: "boot", MountIdentity: "mount-v2"}},
					{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST), EvidenceID: "self-test-v2", Evidence: &capabilityv1.CapabilityEvidence{EvidenceID: "self-test-v2", BootID: "boot", RuntimeName: "runsc", RuntimeBinaryDigest: "binary", ConfigDigest: "runtime-config"}},
				},
			}}, nil
		},
	}
	// The manager requires every dependency reference to resolve inside the
	// same snapshot, so use a base provider as the real source of mount-v2.
	baseKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTestKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	manager, err := NewManager(
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, keys: []*capabilityv1.CapabilityKey{baseKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: baseKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, ObservedAt: timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: &capabilityv1.CapabilityEvidence{EvidenceID: "mount-v2", BootID: "boot", MountIdentity: "mount-v2"}}}, nil
		}},
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, keys: []*capabilityv1.CapabilityKey{selfTestKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: selfTestKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ValidityScope: capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, ObservedAt: timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: &capabilityv1.CapabilityEvidence{EvidenceID: "self-test-v2", BootID: "boot", RuntimeName: "runsc", RuntimeBinaryDigest: "binary", ConfigDigest: "runtime-config"}}}, nil
		}},
		derivedProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	placement := &capabilityv1.CapabilityDependency{
		Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP,
		SelectedEvidence: &capabilityv1.CapabilityEvidence{EvidenceID: "placement"},
	}
	admitted, conditions, err := manager.AdmitDependencies([]*capabilityv1.CapabilityDependency{placement}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(admitted) != 1 || admitted[0].GetSelectedEvidence().GetEvidenceID() != "current" || admitted[0].GetDependencyEvidence()[0].GetEvidence().GetMountIdentity() != "mount-v2" {
		t.Fatalf("admitted dependencies = %#v", admitted)
	}
	if len(conditions) != 1 || conditions[0].GetEvidence().GetEvidenceID() != "current" {
		t.Fatalf("conditions = %#v", conditions)
	}
}
