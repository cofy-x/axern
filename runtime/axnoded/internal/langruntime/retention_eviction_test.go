package langruntime

import (
	"context"
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestTemporaryIdleRuntimeRetainedUntilTTLExpiry(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(200*time.Millisecond, 8)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-retained", "/retained"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	lr.IncRef()
	lr.DecRef()

	if got := lm.GetLangRuntime("rt-retained"); got == nil {
		t.Fatal("expected runtime to remain present while retained")
	}
	if !lr.Retained() {
		t.Fatal("expected runtime to enter retained state")
	}
	if lr.RootFS.RetainedRefCount() != 1 {
		t.Fatalf("retained rootfs refs = %d, want 1", lr.RootFS.RetainedRefCount())
	}
	if mock.UmountCount() != 0 {
		t.Fatalf("expected rootfs to remain mounted while retained, got umounts=%d", mock.UmountCount())
	}

	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(250*time.Millisecond), RetentionReasonTTLExpired)
	lm.executeEvictions(t.Context(), evictions)

	if got := lm.GetLangRuntime("rt-retained"); got != nil {
		t.Fatal("expected runtime to be evicted after TTL expiry")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected rootfs umount after TTL eviction, got %d", mock.UmountCount())
	}
}

func TestTemporaryRuntimeEvictionClearsBundleTemplate(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(100*time.Millisecond, 8)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-template", "/retained-template"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	lr.template = &runtimeoci.BundleTemplate{}
	if lr.template == nil {
		t.Fatal("expected prepared template before eviction")
	}

	lr.IncRef()
	lr.DecRef()
	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(200*time.Millisecond), RetentionReasonTTLExpired)
	lm.executeEvictions(t.Context(), evictions)

	if lr.template != nil {
		t.Fatal("expected bundle template to be cleared on eviction")
	}
}

func TestTemporaryRuntimeEvictionDestroysExecutionEnvelope(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(100*time.Millisecond, 8)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-envelope", "/retained-envelope"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}
	lr.IncRef()
	lr.DecRef()

	destroyCalls := 0
	if !lr.BeginExecutionEnvelopePrepare() {
		t.Fatal("expected execution envelope prepare slot")
	}
	if !lr.FinishExecutionEnvelopePrepare(&ExecutionEnvelope{
		Destroy: func(context.Context) error {
			destroyCalls++
			return nil
		},
	}) {
		t.Fatal("expected execution envelope to become ready")
	}
	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(200*time.Millisecond), RetentionReasonTTLExpired)
	lm.executeEvictions(t.Context(), evictions)

	if destroyCalls != 1 {
		t.Fatalf("execution envelope destroy calls = %d, want 1", destroyCalls)
	}
}

func TestSharedRootfsRetainedUntilLastEviction(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(time.Minute, 8)

	lr1, err := addTestLangRuntime(lm, newTestFR("rt-shared-1", "/shared-retained"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-shared-1 failed: %v", err)
	}
	lr2, err := addTestLangRuntime(lm, newTestFR("rt-shared-2", "/shared-retained"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-shared-2 failed: %v", err)
	}

	lr1.IncRef()
	lr2.IncRef()
	lr1.DecRef()
	lr2.DecRef()

	evictions := lm.collectAllRetained(RetentionReasonCapacity)
	if len(evictions) != 2 {
		t.Fatalf("collectAllRetained() = %d evictions, want 2", len(evictions))
	}

	lm.executeEvictions(t.Context(), evictions[:1])
	if mock.UmountCount() != 0 {
		t.Fatalf("shared rootfs should stay mounted after first eviction, got umounts=%d", mock.UmountCount())
	}

	lm.executeEvictions(t.Context(), evictions[1:])
	if mock.UmountCount() != 1 {
		t.Fatalf("shared rootfs should unmount after last eviction, got umounts=%d", mock.UmountCount())
	}
}

func TestRetentionCapacityEvictsOldestIdleRuntimeFirst(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(time.Minute, 1)

	lrA, err := addTestLangRuntime(lm, newTestFR("rt-cap-a", "/cap-a"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-cap-a failed: %v", err)
	}
	lrA.IncRef()
	lrA.DecRef()

	time.Sleep(10 * time.Millisecond)

	lrB, err := addTestLangRuntime(lm, newTestFR("rt-cap-b", "/cap-b"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-cap-b failed: %v", err)
	}
	lrB.IncRef()
	lrB.DecRef()

	if got := lm.GetLangRuntime("rt-cap-a"); got != nil {
		t.Fatal("expected oldest retained runtime to be evicted when cap is exceeded")
	}
	if got := lm.GetLangRuntime("rt-cap-b"); got == nil {
		t.Fatal("expected newest retained runtime to remain after cap eviction")
	}
	if !lrB.Retained() {
		t.Fatal("expected newest runtime to remain retained")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected exactly one rootfs umount after cap eviction, got %d", mock.UmountCount())
	}
}

func TestDrainRetainedEvictsAllRetainedRuntimes(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(time.Minute, 8)

	lr1, err := addTestLangRuntime(lm, newTestFR("rt-drain-1", "/drain-a"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-drain-1 failed: %v", err)
	}
	lr2, err := addTestLangRuntime(lm, newTestFR("rt-drain-2", "/drain-b"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-drain-2 failed: %v", err)
	}
	lr1.IncRef()
	lr2.IncRef()
	lr1.DecRef()
	lr2.DecRef()

	lm.DrainRetained(t.Context(), RetentionReasonShutdown)

	if got := lm.GetLangRuntime("rt-drain-1"); got != nil {
		t.Fatal("expected first retained runtime to be drained")
	}
	if got := lm.GetLangRuntime("rt-drain-2"); got != nil {
		t.Fatal("expected second retained runtime to be drained")
	}
	if mock.UmountCount() != 2 {
		t.Fatalf("expected both retained rootfs to be umounted, got %d", mock.UmountCount())
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
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(time.Minute, 8)

	runtimeReuseAttrs := retentionReuseAttrs(RetentionReuseKindRuntime, "local")
	rootfsReuseAttrs := retentionReuseAttrs(RetentionReuseKindRootfs, "local")
	runtimeEvictionAttrs := retentionEvictionAttrs(RetentionReuseKindRuntime, "local", RetentionReasonCapacity)
	rootfsEvictionAttrs := retentionEvictionAttrs(RetentionReuseKindRootfs, "local", RetentionReasonCapacity)
	beforeRuntimeReuse := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, runtimeReuseAttrs)
	beforeRootfsReuse := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, rootfsReuseAttrs)
	beforeRuntimeEviction := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, runtimeEvictionAttrs)
	beforeRootfsEviction := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, rootfsEvictionAttrs)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-metrics", "/retention-metrics"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	lr.IncRef()
	lr.DecRef()
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRuntimeCurrent, retainedRootfsAttrs("local")); got != 1 {
		t.Fatalf("retained runtime gauge = %v, want 1", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRootfsCurrent, retainedRootfsAttrs("local")); got != 1 {
		t.Fatalf("retained rootfs gauge = %v, want 1", got)
	}

	lr.IncRef()
	if got := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, runtimeReuseAttrs); got != beforeRuntimeReuse+1 {
		t.Fatalf("runtime reuse counter = %v, want %v", got, beforeRuntimeReuse+1)
	}
	if got := metrics.CounterValueForTest(metrics.MetricRetentionReuseTotal, rootfsReuseAttrs); got != beforeRootfsReuse+1 {
		t.Fatalf("rootfs reuse counter = %v, want %v", got, beforeRootfsReuse+1)
	}

	lr.DecRef()
	lm.DrainRetained(t.Context(), RetentionReasonCapacity)

	if got := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, runtimeEvictionAttrs); got != beforeRuntimeEviction+1 {
		t.Fatalf("runtime eviction counter = %v, want %v", got, beforeRuntimeEviction+1)
	}
	if got := metrics.CounterValueForTest(metrics.MetricRetentionEvictionTotal, rootfsEvictionAttrs); got != beforeRootfsEviction+1 {
		t.Fatalf("rootfs eviction counter = %v, want %v", got, beforeRootfsEviction+1)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRuntimeCurrent, retainedRootfsAttrs("local")); got != 0 {
		t.Fatalf("retained runtime gauge after eviction = %v, want 0", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricRetainedRootfsCurrent, retainedRootfsAttrs("local")); got != 0 {
		t.Fatalf("retained rootfs gauge after eviction = %v, want 0", got)
	}
}
