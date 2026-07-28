package reconcilekernel

import (
	"errors"
	"testing"
	"time"
	"unicode/utf8"
)

func TestHealthTrackerRecordsSuccessAndFailure(t *testing.T) {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	tracker := NewHealthTracker(ComponentRun)
	run := tracker.RecordStart(ComponentRun, start)

	snapshot := tracker.Snapshot()
	if got := snapshot.Components[0]; !got.Running || got.LastStartedAt == nil || !got.LastStartedAt.Equal(start) {
		t.Fatalf("started health = %#v, want running with start time", got)
	}

	failedAt := start.Add(time.Second)
	tracker.RecordResult(run, errors.New("database unavailable"), failedAt)
	snapshot = tracker.Snapshot()
	got := snapshot.Components[0]
	if got.Running || got.ConsecutiveFailures != 1 || got.LastError != "database unavailable" || got.LastErrorAt == nil || !got.LastErrorAt.Equal(failedAt) {
		t.Fatalf("failed health = %#v, want recorded failure", got)
	}

	succeededAt := failedAt.Add(time.Second)
	run = tracker.RecordStart(ComponentRun, failedAt)
	tracker.RecordResult(run, nil, succeededAt)
	snapshot = tracker.Snapshot()
	got = snapshot.Components[0]
	if got.Running || got.ConsecutiveFailures != 0 || got.LastError != "database unavailable" || got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(succeededAt) {
		t.Fatalf("recovered health = %#v, want success with preserved last error", got)
	}
}

func TestEmptyHealthSnapshotUsesStableArrayShape(t *testing.T) {
	if EmptyHealthSnapshot().Components == nil {
		t.Fatal("EmptyHealthSnapshot().Components = nil, want empty slice")
	}
	if (*HealthTracker)(nil).Snapshot().Components == nil {
		t.Fatal("nil tracker Snapshot().Components = nil, want empty slice")
	}
}

func TestHealthTrackerBoundsLastError(t *testing.T) {
	tracker := NewHealthTracker(ComponentAllocation)
	run := tracker.RecordStart(ComponentAllocation, time.Now())
	tracker.RecordResult(run, errors.New(string(make([]byte, maxLastErrorBytes+100))+"错误"), time.Now())
	got := tracker.Snapshot().Components[0].LastError
	if len(got) > maxLastErrorBytes+3 || got[len(got)-3:] != "..." || !utf8.ValidString(got) {
		t.Fatalf("last error length = %d, want bounded valid UTF-8 with ellipsis", len(got))
	}
}

func TestHealthTrackerKeepsRunningUntilConcurrentWorkDrains(t *testing.T) {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	tracker := NewHealthTracker(ComponentService)
	first := tracker.RecordStart(ComponentService, start)
	second := tracker.RecordStart(ComponentService, start.Add(time.Second))
	tracker.RecordResult(first, nil, start.Add(2*time.Second))
	got := tracker.Snapshot().Components[0]
	if !got.Running || got.RunningSince == nil || !got.RunningSince.Equal(start) {
		t.Fatalf("partially drained health = %#v, want continuously running since first start", got)
	}
	tracker.RecordResult(second, nil, start.Add(3*time.Second))
	got = tracker.Snapshot().Components[0]
	if got.Running || got.RunningSince != nil {
		t.Fatalf("drained health = %#v, want idle", got)
	}
}

func TestHealthTrackerHandlesOutOfOrderConcurrentEvents(t *testing.T) {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	tracker := NewHealthTracker(ComponentService)
	later := tracker.RecordStart(ComponentService, start.Add(time.Second))
	earlier := tracker.RecordStart(ComponentService, start)

	got := tracker.Snapshot().Components[0]
	if got.RunningSince == nil || !got.RunningSince.Equal(start) || got.LastStartedAt == nil || !got.LastStartedAt.Equal(start.Add(time.Second)) {
		t.Fatalf("out-of-order starts = %#v, want earliest continuous and latest start times", got)
	}

	tracker.RecordResult(later, nil, start.Add(4*time.Second))
	tracker.RecordResult(earlier, errors.New("older result"), start.Add(3*time.Second))
	got = tracker.Snapshot().Components[0]
	if got.Running || got.ConsecutiveFailures != 0 || got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(start.Add(4*time.Second)) {
		t.Fatalf("out-of-order results = %#v, want latest completion to remain authoritative", got)
	}
}

func TestCountUnhealthyComponentsIncludesStuckRunningWork(t *testing.T) {
	now := time.Date(2026, 5, 10, 10, 1, 0, 0, time.UTC)
	started := now.Add(-time.Minute)
	unhealthy, stuck := CountUnhealthyComponents(HealthSnapshot{Components: []ComponentHealth{
		{Component: ComponentRun, ConsecutiveFailures: 1},
		{Component: ComponentNode, Running: true, RunningSince: &started},
	}}, now, 30*time.Second)
	if unhealthy != 2 || stuck != 1 {
		t.Fatalf("unhealthy=%d stuck=%d, want 2/1", unhealthy, stuck)
	}
}
