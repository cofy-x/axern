package appadmin

import (
	"context"
	"errors"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
)

func TestReliabilityHealthIncludesStorageBindings(t *testing.T) {
	control := NewReliabilityControl(
		fakeReliabilityStore{},
		nil,
		time.Minute,
		fakeStorageHealth{health: adminkernel.StorageBindingHealth{FailedBindings: 2, ReleasingBindings: 1, StuckReleasingBindings: 1}},
		nil,
	)
	health, err := control.Health(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != adminkernel.ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 1 || health.Signals[0].Code != adminkernel.ReliabilitySignalStorageBindings {
		t.Fatalf("signals = %+v, want storage binding signal", health.Signals)
	}
	if health.StorageBindingHealth.FailedBindings != 2 || health.StorageBindingHealth.ReleasingBindings != 1 || health.StorageBindingHealth.StuckReleasingBindings != 1 {
		t.Fatalf("storage binding health = %+v", health.StorageBindingHealth)
	}
}

func TestReliabilityHealthTreatsStorageHealthErrorAsSignal(t *testing.T) {
	control := NewReliabilityControl(
		fakeReliabilityStore{},
		nil,
		time.Minute,
		fakeStorageHealth{err: errors.New("storaged unavailable")},
		nil,
	)
	health, err := control.Health(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != adminkernel.ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 1 || health.Signals[0].Code != adminkernel.ReliabilitySignalStorageBindings {
		t.Fatalf("signals = %+v, want storage binding signal", health.Signals)
	}
	if !health.StorageBindingHealth.Unavailable || health.StorageBindingHealth.Error != "storaged unavailable" {
		t.Fatalf("storage binding health = %+v, want unavailable storaged error", health.StorageBindingHealth)
	}
}

func TestReliabilityHealthIncludesNodeVolumeHealth(t *testing.T) {
	control := NewReliabilityControl(
		fakeReliabilityStore{},
		nil,
		time.Minute,
		fakeStorageHealth{},
		fakeNodeVolumeHealth{health: adminkernel.NodeVolumeHealth{UnhealthyNodes: 1, Error: "volumed reconcile failed"}},
	)
	health, err := control.Health(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != adminkernel.ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 1 || health.Signals[0].Code != adminkernel.ReliabilitySignalNodeVolumeManagers {
		t.Fatalf("signals = %+v, want node volume manager signal", health.Signals)
	}
	if health.NodeVolumeHealth.UnhealthyNodes != 1 || health.NodeVolumeHealth.Error != "volumed reconcile failed" {
		t.Fatalf("node volume health = %+v", health.NodeVolumeHealth)
	}
}

func TestReliabilityHealthTreatsNodeHealthErrorAsFleetSignal(t *testing.T) {
	control := NewReliabilityControl(fakeReliabilityStore{}, nil, time.Minute, fakeStorageHealth{}, fakeNodeVolumeHealth{err: errors.New("nodes unavailable")})
	health, err := control.Health(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != adminkernel.ReliabilityStatusDegraded || len(health.Signals) != 1 || health.Signals[0].Code != adminkernel.ReliabilitySignalNodeFleet {
		t.Fatalf("health = %+v, want node fleet signal", health)
	}
	if !health.NodeFleetHealth.Unavailable || health.NodeFleetHealth.Error != "nodes unavailable" || health.NodeVolumeHealth.UnhealthyNodes != 0 {
		t.Fatalf("node health = fleet %+v, volumes %+v", health.NodeFleetHealth, health.NodeVolumeHealth)
	}
}

type fakeReliabilityStore struct{}

func (fakeReliabilityStore) ConsistencySnapshot(context.Context, time.Time) (consistencykernel.Snapshot, error) {
	return consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false), nil
}

func (fakeReliabilityStore) CountAllocationLifecycleRetries(context.Context, time.Time) (adminkernel.AllocationLifecycleRetryCounts, error) {
	return adminkernel.AllocationLifecycleRetryCounts{}, nil
}

type fakeStorageHealth struct {
	health adminkernel.StorageBindingHealth
	err    error
}

func (f fakeStorageHealth) StorageBindingHealth(context.Context, time.Duration) (adminkernel.StorageBindingHealth, error) {
	if f.err != nil {
		return adminkernel.StorageBindingHealth{}, f.err
	}
	return f.health, nil
}

type fakeNodeVolumeHealth struct {
	fleet  adminkernel.NodeFleetHealth
	health adminkernel.NodeVolumeHealth
	err    error
}

func (f fakeNodeVolumeHealth) NodeHealth(context.Context, time.Time) (adminkernel.NodeFleetHealth, adminkernel.NodeVolumeHealth, error) {
	if f.err != nil {
		return adminkernel.NodeFleetHealth{}, adminkernel.NodeVolumeHealth{}, f.err
	}
	return f.fleet, f.health, nil
}
