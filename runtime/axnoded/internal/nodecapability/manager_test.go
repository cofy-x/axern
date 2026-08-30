package nodecapability

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const testBootID = "11111111-2222-3333-4444-555555555555"

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

type testDeriver struct{ testProvider }

func (p testDeriver) Derive(_ context.Context, now time.Time, _ map[string]*capabilityv1.CapabilityObservation) ([]*capabilityv1.CapabilityObservation, error) {
	return p.observe(now)
}

type serialPublicationMetrics struct {
	active  atomic.Int32
	maximum atomic.Int32
}

func (*serialPublicationMetrics) RecordProviderProbe(capabilityv1.CapabilityProvider, string, time.Duration) {
}
func (m *serialPublicationMetrics) RecordSnapshot(*capabilityv1.CapabilitySnapshot) {
	current := m.active.Add(1)
	defer m.active.Add(-1)
	for {
		seen := m.maximum.Load()
		if current <= seen || m.maximum.CompareAndSwap(seen, current) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
}
func (*serialPublicationMetrics) RecordTransitions([]*Transition) {}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }

func networkObservation(key *capabilityv1.CapabilityKey, now time.Time, state capabilityv1.CapabilityState) *capabilityv1.CapabilityObservation {
	reason := capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
	if state != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		reason = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	}
	return &capabilityv1.CapabilityObservation{
		Key: key, State: state, ObservedAt: timestamppb.New(now),
		ValidUntil: timestamppb.New(now.Add(capabilitycontract.HealthObservationValidity)),
		ReasonCode: reason, Evidence: capabilitycontract.ConfigEvidence(digest("a")),
	}
}

func TestManagerRejectsDuplicateOwnership(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) { return nil, nil }}
	if _, err := NewManager(provider, provider); err == nil {
		t.Fatal("NewManager accepted duplicate capability ownership")
	}
}

func TestValidateCatalogProviderCoverageRejectsMissingPlatformProvider(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}}
	if err := ValidateCatalogProviderCoverage(provider); err == nil {
		t.Fatal("partial production provider catalog was accepted")
	}
}

func TestManagerPublishesAtomicUnknownForProviderError(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return nil, errors.New("probe unavailable")
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Refresh(context.Background(), time.Now().UTC())
	if err != nil || !manager.Ready() || len(snapshot.GetObservations()) != 1 {
		t.Fatalf("snapshot=%#v error=%v ready=%v", snapshot, err, manager.Ready())
	}
	if observation := snapshot.GetObservations()[0]; observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN || observation.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR {
		t.Fatalf("provider failure observation = %#v", observation)
	}
}

func TestManagerSerializesOneProviderWithoutBlockingIndependentProviders(t *testing.T) {
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
		return []*capabilityv1.CapabilityObservation{networkObservation(key, now, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
	}}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() { defer wait.Done(); _, _ = manager.Refresh(context.Background(), time.Now().UTC()) }()
	}
	wait.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("concurrent samples = %d, want 1", maximum.Load())
	}
}

func TestManagerSerializesSnapshotPublicationAndObserverDelivery(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	manager, err := NewManager(testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		keys:     []*capabilityv1.CapabilityKey{key},
		observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{networkObservation(key, now, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	metrics := &serialPublicationMetrics{}
	manager.SetMetricsObserver(metrics)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := manager.publish(context.Background(), now.Add(time.Second)); err != nil {
				t.Errorf("publish: %v", err)
			}
		}()
	}
	wait.Wait()
	if metrics.maximum.Load() != 1 {
		t.Fatalf("concurrent snapshot observer deliveries = %d, want 1", metrics.maximum.Load())
	}
}

func TestSlowTransitionHandlerDoesNotBlockSnapshotPublication(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	state := capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	manager, err := NewManager(testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		keys:     []*capabilityv1.CapabilityKey{key},
		observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{networkObservation(key, now, state)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	manager.Subscribe(func(context.Context, []*Transition) {
		close(handlerStarted)
		<-releaseHandler
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.startDeliveryWorker(ctx)

	state = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	if _, err := manager.Refresh(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("transition handler was not invoked")
	}

	published := make(chan error, 1)
	go func() {
		for offset := 2; offset < 34; offset++ {
			if _, err := manager.publish(context.Background(), now.Add(time.Duration(offset)*time.Second)); err != nil {
				published <- err
				return
			}
		}
		published <- nil
	}()
	select {
	case err := <-published:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("slow transition delivery blocked a later snapshot publication")
	}
	manager.deliveryMu.Lock()
	queued := len(manager.deliveryQueue)
	manager.deliveryMu.Unlock()
	if queued != 1 {
		t.Fatalf("queued no-transition deliveries = %d, want one coalesced latest snapshot", queued)
	}
	close(releaseHandler)
}

func TestTransitionHandlerPanicDoesNotSuppressFollowingHandler(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	state := capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	manager, err := NewManager(testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		keys:     []*capabilityv1.CapabilityKey{key},
		observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{networkObservation(key, now, state)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	manager.Subscribe(func(context.Context, []*Transition) { panic("broken subscriber") })
	delivered := make(chan struct{}, 1)
	manager.Subscribe(func(context.Context, []*Transition) { delivered <- struct{}{} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.startDeliveryWorker(ctx)

	state = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	if _, err := manager.Refresh(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("a panicking transition handler suppressed the following handler")
	}
}

func TestRuntimeProviderSerialLaneHonorsCancellationWhileQueued(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST)
	called := false
	provider := testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST,
		keys:     []*capabilityv1.CapabilityKey{key},
		observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			called = true
			return nil, nil
		},
	}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	<-manager.runtimeProbeSerial
	defer func() { manager.runtimeProbeSerial <- struct{}{} }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.observeSafely(ctx, provider, time.Now().UTC()); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued runtime provider error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("queued runtime provider ran after its scheduler context was canceled")
	}
}

func TestRuntimeProvidersShareOneGlobalSerialLane(t *testing.T) {
	var active, maximum atomic.Int32
	observe := func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}
	runcProvider := testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST,
		keys:     []*capabilityv1.CapabilityKey{capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST)},
		observe:  observe,
	}
	runscProvider := testProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST,
		keys:     []*capabilityv1.CapabilityKey{capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)},
		observe:  observe,
	}
	manager, err := NewManager(runcProvider, runscProvider)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for _, provider := range []Provider{runcProvider, runscProvider} {
		provider := provider
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = manager.observeSafely(context.Background(), provider, time.Now().UTC())
		}()
	}
	wait.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("concurrent runtime certifications = %d, want 1", maximum.Load())
	}
}

func TestSlowRuntimeProviderDoesNotBlockHealthPublication(t *testing.T) {
	runtimeKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST)
	networkKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	releaseRuntime := make(chan struct{})
	networkPublished := make(chan struct{}, 1)
	manager, err := NewManager(
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST, keys: []*capabilityv1.CapabilityKey{runtimeKey}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			<-releaseRuntime
			return []*capabilityv1.CapabilityObservation{{Key: runtimeKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: capabilitycontract.RuntimeEvidence(testBootID, "runc", digest("b"), digest("c"))}}, nil
		}},
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{networkKey}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			select {
			case networkPublished <- struct{}{}:
			default:
			}
			return []*capabilityv1.CapabilityObservation{networkObservation(networkKey, now, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)
	select {
	case <-networkPublished:
	case <-time.After(time.Second):
		t.Fatal("health provider was blocked by runtime conformance")
	}
	var snapshot *capabilityv1.CapabilitySnapshot
	deadline := time.Now().Add(time.Second)
	for snapshot == nil && time.Now().Before(deadline) {
		snapshot = manager.Snapshot()
		time.Sleep(time.Millisecond)
	}
	if snapshot == nil || len(snapshot.GetObservations()) != 1 || snapshot.GetObservations()[0].GetKey().GetPlatform() != capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING {
		t.Fatalf("health publication = %#v", snapshot)
	}
	if manager.Ready() {
		t.Fatal("manager became READY before slow provider produced its first batch")
	}
	close(releaseRuntime)
}

func TestDerivedCapabilityDoesNotEnterRecoveryDuringInitialWarming(t *testing.T) {
	filestoreKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTestKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	derivedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	releaseSelfTest := make(chan struct{})
	manager, err := NewManager(
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, keys: []*capabilityv1.CapabilityKey{filestoreKey}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: filestoreKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampledAt), ValidUntil: timestamppb.New(sampledAt.Add(capabilitycontract.HealthObservationValidity)), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: capabilitycontract.MountEvidence(testBootID, "42:/filestore:xfs")}}, nil
		}},
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, keys: []*capabilityv1.CapabilityKey{selfTestKey}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			<-releaseSelfTest
			return []*capabilityv1.CapabilityObservation{{Key: selfTestKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampledAt), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: capabilitycontract.RuntimeEvidence(testBootID, "runsc", digest("b"), digest("c"))}}, nil
		}},
		testDeriver{testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, keys: []*capabilityv1.CapabilityKey{derivedKey}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: derivedKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampledAt), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE}}, nil
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)
	deadline := time.Now().Add(time.Second)
	for manager.Snapshot() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Ready() {
		t.Fatal("manager became ready before every base provider completed")
	}
	close(releaseSelfTest)
	for !manager.Ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !manager.Ready() {
		t.Fatal("manager did not become ready after every base provider completed")
	}
	observation, ok := capabilitycontract.AvailableObservation(manager.Snapshot(), derivedKey, time.Now().UTC())
	if !ok || observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatalf("first complete derived observation = %#v, want AVAILABLE without recovery debounce", observation)
	}
}

func TestManagerRejectsStalePublicationTime(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return []*capabilityv1.CapabilityObservation{networkObservation(key, now, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now.Add(-time.Second)); err == nil {
		t.Fatal("Refresh accepted a timestamp older than the published snapshot")
	}
}

func TestManagerRecoveryRequiresTwoIndependentSamples(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	state := capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	observedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	provider := testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return []*capabilityv1.CapabilityObservation{networkObservation(key, observedAt, state)}, nil
	}}
	manager, err := NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = manager.Refresh(context.Background(), observedAt)
	state = capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	first, _ := manager.Refresh(context.Background(), observedAt.Add(5*time.Second))
	if first.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatal("first successful recovery sample was not debounced")
	}
	second, _ := manager.Refresh(context.Background(), observedAt.Add(10*time.Second))
	if second.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatal("republication of the same sample completed recovery")
	}
	observedAt = observedAt.Add(10 * time.Second)
	third, _ := manager.Refresh(context.Background(), observedAt.Add(5*time.Second))
	if third.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatal("second independent recovery sample did not restore availability")
	}
}

func TestDerivedCapabilityRecoversWithConfirmedBaseObservations(t *testing.T) {
	filestoreKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTestKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	derivedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	filestoreState := capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	filestoreObservedAt := now
	manager, err := NewManager(
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, keys: []*capabilityv1.CapabilityKey{filestoreKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			observation := &capabilityv1.CapabilityObservation{
				Key: filestoreKey, State: filestoreState, ObservedAt: timestamppb.New(filestoreObservedAt),
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED,
				Reason:     "filestore unavailable", Evidence: capabilitycontract.MountEvidence(testBootID, "42:/filestore:xfs"),
			}
			if filestoreState == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
				observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
				observation.Reason = ""
				observation.ValidUntil = timestamppb.New(filestoreObservedAt.Add(capabilitycontract.HealthObservationValidity))
			}
			return []*capabilityv1.CapabilityObservation{observation}, nil
		}},
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, keys: []*capabilityv1.CapabilityKey{selfTestKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{
				Key: selfTestKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
				ObservedAt: timestamppb.New(now),
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
				Evidence:   capabilitycontract.RuntimeEvidence(testBootID, "runsc", digest("b"), digest("c")),
			}}, nil
		}},
		testDeriver{testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, keys: []*capabilityv1.CapabilityKey{derivedKey}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: derivedKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampledAt), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE}}, nil
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	filestoreState = capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	filestoreObservedAt = now.Add(5 * time.Second)
	first, err := manager.Refresh(context.Background(), filestoreObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, available := capabilitycontract.AvailableObservation(first, derivedKey, filestoreObservedAt); available {
		t.Fatal("derived capability recovered before the base provider's second independent success")
	}
	filestoreObservedAt = now.Add(10 * time.Second)
	second, err := manager.Refresh(context.Background(), filestoreObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if observation, available := capabilitycontract.AvailableObservation(second, derivedKey, filestoreObservedAt); !available || observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatalf("derived observation = %#v, want immediate availability after base recovery", observation)
	}
}

func TestManagerErrorRecoveryRequiresTwoIndependentSamples(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	failing := true
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		if failing {
			return nil, errors.New("probe unavailable")
		}
		return []*capabilityv1.CapabilityObservation{networkObservation(key, sampledAt, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	failing = false
	first, err := manager.Refresh(context.Background(), now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatal("first success after provider error bypassed recovery debounce")
	}
	second, err := manager.Refresh(context.Background(), now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatal("second independent success did not recover provider error")
	}
}

func TestManagerExpiryRecoveryRequiresTwoIndependentSamples(t *testing.T) {
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, keys: []*capabilityv1.CapabilityKey{key}, observe: func(sampledAt time.Time) ([]*capabilityv1.CapabilityObservation, error) {
		return []*capabilityv1.CapabilityObservation{networkObservation(key, sampledAt, capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	expired, err := manager.publish(context.Background(), now.Add(capabilitycontract.HealthObservationValidity))
	if err != nil {
		t.Fatal(err)
	}
	if expired.GetObservations()[0].GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED {
		t.Fatal("expired observation did not fail closed")
	}
	first, err := manager.Refresh(context.Background(), now.Add(20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED {
		t.Fatal("first success after expiry bypassed recovery debounce")
	}
	second, err := manager.Refresh(context.Background(), now.Add(25*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.GetObservations()[0].GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		t.Fatal("second independent success did not recover expiry")
	}
}

func TestManagerAdmitsCurrentCompleteProof(t *testing.T) {
	baseKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTestKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	derivedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	sampleTime := now
	manager, err := NewManager(
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, keys: []*capabilityv1.CapabilityKey{baseKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: baseKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampleTime), ValidUntil: timestamppb.New(sampleTime.Add(capabilitycontract.HealthObservationValidity)), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: capabilitycontract.MountEvidence(testBootID, "42:/filestore:xfs")}}, nil
		}},
		testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, keys: []*capabilityv1.CapabilityKey{selfTestKey}, observe: func(time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: selfTestKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(sampleTime), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Evidence: capabilitycontract.RuntimeEvidence(testBootID, "runsc", digest("b"), digest("c"))}}, nil
		}},
		testDeriver{testProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, keys: []*capabilityv1.CapabilityKey{derivedKey}, observe: func(now time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{{Key: derivedKey, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, ObservedAt: timestamppb.New(now), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE}}, nil
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Refresh(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	placement, err := capabilitycontract.ResolveDependencies(first, []*capabilityv1.CapabilityKey{derivedKey}, now)
	if err != nil {
		t.Fatal(err)
	}
	sampleTime = now.Add(5 * time.Second)
	if _, err := manager.Refresh(context.Background(), sampleTime); err != nil {
		t.Fatal(err)
	}
	admitted, conditions, err := manager.AdmitDependencies(placement, sampleTime)
	if err != nil {
		t.Fatal(err)
	}
	if admitted[0].GetSelectedObservation().GetObservationID() == placement[0].GetSelectedObservation().GetObservationID() {
		t.Fatal("admission did not bind the current observation")
	}
	if len(admitted[0].GetDependencyObservations()) != 2 || conditions[0].GetProof().GetObservationID() != admitted[0].GetSelectedObservation().GetObservationID() {
		t.Fatalf("admitted proof is incomplete: admitted=%#v conditions=%#v", admitted, conditions)
	}
}
