package appadmin

import (
	"context"
	"errors"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
)

func TestReliabilityHealthTreatsNodeHealthErrorAsFleetSignal(t *testing.T) {
	control := NewReliabilityControl(fakeReliabilityStore{}, nil, time.Minute, fakeNodeHealth{err: errors.New("nodes unavailable")})
	health, err := control.Health(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != adminkernel.ReliabilityStatusDegraded || len(health.Signals) != 1 || health.Signals[0].Code != adminkernel.ReliabilitySignalNodeFleet {
		t.Fatalf("health = %+v, want node fleet signal", health)
	}
	if !health.NodeFleetHealth.Unavailable || health.NodeFleetHealth.Error != "nodes unavailable" {
		t.Fatalf("node health = fleet %+v", health.NodeFleetHealth)
	}
}

type fakeReliabilityStore struct{}

func (fakeReliabilityStore) ConsistencySnapshot(context.Context, time.Time) (consistencykernel.Snapshot, error) {
	return consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false), nil
}

func (fakeReliabilityStore) CountAllocationLifecycleRetries(context.Context, time.Time) (adminkernel.AllocationLifecycleRetryCounts, error) {
	return adminkernel.AllocationLifecycleRetryCounts{}, nil
}

type fakeNodeHealth struct {
	fleet adminkernel.NodeFleetHealth
	err   error
}

func (f fakeNodeHealth) NodeHealth(context.Context, time.Time) (adminkernel.NodeFleetHealth, error) {
	if f.err != nil {
		return adminkernel.NodeFleetHealth{}, f.err
	}
	return f.fleet, nil
}
