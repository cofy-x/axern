package environmentcache

import (
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestTemporaryIdleEnvironmentRetainedUntilTTLExpiry(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(200*time.Millisecond, 8)

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-retained", "/retained"))
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}

	environment.IncRef()
	environment.DecRef()

	if got := lm.GetPreparedEnvironment("rt-retained"); got == nil {
		t.Fatal("expected runtime to remain present while retained")
	}
	if !environment.Retained() {
		t.Fatal("expected runtime to enter retained state")
	}
	if environment.RootFS.RetainedRefCount() != 1 {
		t.Fatalf("retained rootfs refs = %d, want 1", environment.RootFS.RetainedRefCount())
	}
	if mock.UmountCount() != 0 {
		t.Fatalf("expected rootfs to remain mounted while retained, got umounts=%d", mock.UmountCount())
	}

	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(250*time.Millisecond), RetentionReasonTTLExpired)
	lm.executeEvictions(evictions)

	if got := lm.GetPreparedEnvironment("rt-retained"); got != nil {
		t.Fatal("expected runtime to be evicted after TTL expiry")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected rootfs umount after TTL eviction, got %d", mock.UmountCount())
	}
}

func TestTemporaryRuntimeEvictionClearsBundleTemplate(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(100*time.Millisecond, 8)

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-template", "/retained-template"))
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}

	environment.template = &runtimeoci.BundleTemplate{}
	if environment.template == nil {
		t.Fatal("expected prepared template before eviction")
	}

	environment.IncRef()
	environment.DecRef()
	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(200*time.Millisecond), RetentionReasonTTLExpired)
	lm.executeEvictions(evictions)

	if environment.template != nil {
		t.Fatal("expected bundle template to be cleared on eviction")
	}
}

func TestSharedRootfsRetainedUntilLastEviction(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 8)

	lr1, err := addTestEnvironmentCache(lm, newTestFR("rt-shared-1", "/shared-retained"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-shared-1 failed: %v", err)
	}
	lr2, err := addTestEnvironmentCache(lm, newTestFR("rt-shared-2", "/shared-retained"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-shared-2 failed: %v", err)
	}

	lr1.IncRef()
	lr2.IncRef()
	lr1.DecRef()
	lr2.DecRef()

	evictions := lm.collectAllRetained(RetentionReasonCapacity)
	if len(evictions) != 2 {
		t.Fatalf("collectAllRetained() = %d evictions, want 2", len(evictions))
	}

	lm.executeEvictions(evictions[:1])
	if mock.UmountCount() != 0 {
		t.Fatalf("shared rootfs should stay mounted after first eviction, got umounts=%d", mock.UmountCount())
	}

	lm.executeEvictions(evictions[1:])
	if mock.UmountCount() != 1 {
		t.Fatalf("shared rootfs should unmount after last eviction, got umounts=%d", mock.UmountCount())
	}
}

func TestRetentionCapacityEvictsOldestIdleEnvironmentFirst(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 1)

	lrA, err := addTestEnvironmentCache(lm, newTestFR("rt-cap-a", "/cap-a"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-cap-a failed: %v", err)
	}
	lrA.IncRef()
	lrA.DecRef()

	time.Sleep(10 * time.Millisecond)

	lrB, err := addTestEnvironmentCache(lm, newTestFR("rt-cap-b", "/cap-b"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-cap-b failed: %v", err)
	}
	lrB.IncRef()
	lrB.DecRef()

	if got := lm.GetPreparedEnvironment("rt-cap-a"); got != nil {
		t.Fatal("expected oldest retained environment to be evicted when cap is exceeded")
	}
	if got := lm.GetPreparedEnvironment("rt-cap-b"); got == nil {
		t.Fatal("expected newest retained environment to remain after cap eviction")
	}
	if !lrB.Retained() {
		t.Fatal("expected newest runtime to remain retained")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected exactly one rootfs umount after cap eviction, got %d", mock.UmountCount())
	}
}

func TestDrainRetainedEvictsAllRetainedEnvironments(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 8)

	lr1, err := addTestEnvironmentCache(lm, newTestFR("rt-drain-1", "/drain-a"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-drain-1 failed: %v", err)
	}
	lr2, err := addTestEnvironmentCache(lm, newTestFR("rt-drain-2", "/drain-b"))
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-drain-2 failed: %v", err)
	}
	lr1.IncRef()
	lr2.IncRef()
	lr1.DecRef()
	lr2.DecRef()

	lm.DrainRetained(RetentionReasonShutdown)

	if got := lm.GetPreparedEnvironment("rt-drain-1"); got != nil {
		t.Fatal("expected first retained environment to be drained")
	}
	if got := lm.GetPreparedEnvironment("rt-drain-2"); got != nil {
		t.Fatal("expected second retained environment to be drained")
	}
	if mock.UmountCount() != 2 {
		t.Fatalf("expected both retained rootfs to be umounted, got %d", mock.UmountCount())
	}
}

func TestEvictIdleEnvironmentOnlyEvictsSelectedRuntime(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 2)
	first, err := addTestEnvironmentCache(lm, newTestFR("first", "/first"))
	if err != nil {
		t.Fatalf("PrepareEnvironment(first) error = %v", err)
	}
	second, err := addTestEnvironmentCache(lm, newTestFR("second", "/second"))
	if err != nil {
		t.Fatalf("PrepareEnvironment(second) error = %v", err)
	}

	first.IncRef()
	first.DecRef()
	second.IncRef()
	second.DecRef()

	if err := lm.EvictIdleEnvironment(first.ID, RetentionReasonSelfTest); err != nil {
		t.Fatalf("EvictIdleEnvironment() error = %v", err)
	}
	if lm.GetPreparedEnvironment(first.ID) != nil {
		t.Fatalf("runtime %q was not evicted", first.ID)
	}
	if lm.GetPreparedEnvironment(second.ID) != second {
		t.Fatalf("unrelated runtime %q was evicted", second.ID)
	}
	if first.RootFS.RetainedRefCount() != 0 {
		t.Fatalf("selected rootfs retained refs = %d, want 0", first.RootFS.RetainedRefCount())
	}
	if second.RootFS.RetainedRefCount() != 1 {
		t.Fatalf("unrelated rootfs retained refs = %d, want 1", second.RootFS.RetainedRefCount())
	}
}

func retainedRootfsAttrs(rootfsType string) map[string]string {
	return map[string]string{sdkobs.AttrRootFSType: rootfsType}
}

func retentionReuseAttrs(kind, rootfsType string) map[string]string {
	return map[string]string{
		sdkobs.AttrKind:       kind,
		sdkobs.AttrRootFSType: rootfsType,
	}
}

func retentionEvictionAttrs(kind, rootfsType, reason string) map[string]string {
	return map[string]string{
		sdkobs.AttrKind:       kind,
		sdkobs.AttrRootFSType: rootfsType,
		sdkobs.AttrReason:     reason,
	}
}

func TestRetentionMetricsReflectReuseAndEviction(t *testing.T) {
	metrics.ResetForTest()

	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 8)

	runtimeReuseAttrs := retentionReuseAttrs(RetentionReuseKindEnvironment, "local")
	rootfsReuseAttrs := retentionReuseAttrs(RetentionReuseKindRootfs, "local")
	runtimeEvictionAttrs := retentionEvictionAttrs(RetentionReuseKindEnvironment, "local", RetentionReasonCapacity)
	rootfsEvictionAttrs := retentionEvictionAttrs(RetentionReuseKindRootfs, "local", RetentionReasonCapacity)
	beforeRuntimeReuse := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, runtimeReuseAttrs)
	beforeRootfsReuse := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, rootfsReuseAttrs)
	beforeRuntimeEviction := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, runtimeEvictionAttrs)
	beforeRootfsEviction := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, rootfsEvictionAttrs)

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-metrics", "/retention-metrics"))
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}

	environment.IncRef()
	environment.DecRef()
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedEnvironmentCurrent, retainedRootfsAttrs("local")); got != 1 {
		t.Fatalf("retained environment gauge = %v, want 1", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRootfsCurrent, retainedRootfsAttrs("local")); got != 1 {
		t.Fatalf("retained rootfs gauge = %v, want 1", got)
	}

	environment.IncRef()
	if got := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, runtimeReuseAttrs); got != beforeRuntimeReuse+1 {
		t.Fatalf("runtime reuse counter = %v, want %v", got, beforeRuntimeReuse+1)
	}
	if got := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, rootfsReuseAttrs); got != beforeRootfsReuse+1 {
		t.Fatalf("rootfs reuse counter = %v, want %v", got, beforeRootfsReuse+1)
	}

	environment.DecRef()
	lm.DrainRetained(RetentionReasonCapacity)

	if got := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, runtimeEvictionAttrs); got != beforeRuntimeEviction+1 {
		t.Fatalf("runtime eviction counter = %v, want %v", got, beforeRuntimeEviction+1)
	}
	if got := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, rootfsEvictionAttrs); got != beforeRootfsEviction+1 {
		t.Fatalf("rootfs eviction counter = %v, want %v", got, beforeRootfsEviction+1)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedEnvironmentCurrent, retainedRootfsAttrs("local")); got != 0 {
		t.Fatalf("retained environment gauge after eviction = %v, want 0", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRootfsCurrent, retainedRootfsAttrs("local")); got != 0 {
		t.Fatalf("retained rootfs gauge after eviction = %v, want 0", got)
	}
}
