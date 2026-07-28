package nodekernel

import (
	"testing"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestReportedActiveInstancesUsesStrongestOccupancySignal(t *testing.T) {
	summary := &nodev1.NodeSummary{
		Components: &nodev1.ComponentsSummary{Axnoded: &nodev1.AxnodedSummary{
			ActiveAllocationIds: []string{"a", "b"},
		}},
		Pools: &nodev1.PoolsSummary{
			Cgroup:       &nodev1.PoolState{Using: 3},
			Interface:    &nodev1.PoolState{Using: 4},
			RuntimeSlots: &nodev1.PoolState{Using: 4},
		},
	}
	if got := ReportedActiveInstances(summary); got != 4 {
		t.Fatalf("ReportedActiveInstances() = %d, want 4", got)
	}
}

func TestRuntimeSlotCapacityUsesNodeAggregateContract(t *testing.T) {
	summary := &nodev1.NodeSummary{Pools: &nodev1.PoolsSummary{
		RuntimeSlots: &nodev1.PoolState{Capacity: 80, Unavailable: 18},
	}}
	capacity, known := RuntimeSlotCapacity(summary)
	if !known || capacity != 62 {
		t.Fatalf("RuntimeSlotCapacity() = %d/%v, want 62/true", capacity, known)
	}
}

func TestRuntimeSlotCapacityTreatsReportedZeroAsExhausted(t *testing.T) {
	summary := &nodev1.NodeSummary{Pools: &nodev1.PoolsSummary{
		RuntimeSlots: &nodev1.PoolState{},
	}}
	capacity, known := RuntimeSlotCapacity(summary)
	if !known || capacity != 0 {
		t.Fatalf("RuntimeSlotCapacity() = %d/%v, want 0/true", capacity, known)
	}
}

func TestCalculateRuntimeSlotOccupancyUnionsAllocationOwnership(t *testing.T) {
	tests := []struct {
		name     string
		reserved []string
		active   []string
		using    int32
		want     RuntimeSlotOccupancy
	}{
		{
			name:     "overlapping reservations and active allocations are counted once",
			reserved: []string{"a", "b"}, active: []string{"a"}, using: 1,
			want: RuntimeSlotOccupancy{Reserved: 2, Active: 1, PoolUsing: 1, Occupied: 2},
		},
		{
			name:     "released reservation with active sandbox remains occupied",
			reserved: []string{"new"}, active: []string{"old"}, using: 2,
			want: RuntimeSlotOccupancy{Reserved: 1, Active: 1, PoolUsing: 2, Occupied: 2},
		},
		{
			name:     "anonymous pool usage remains conservative",
			reserved: []string{"a"}, active: []string{"a"}, using: 2,
			want: RuntimeSlotOccupancy{Reserved: 1, Active: 1, PoolUsing: 2, Occupied: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &nodev1.NodeSummary{
				Components: &nodev1.ComponentsSummary{Axnoded: &nodev1.AxnodedSummary{ActiveAllocationIds: tt.active}},
				Pools: &nodev1.PoolsSummary{
					RuntimeSlots: &nodev1.PoolState{Using: tt.using},
				},
			}
			if got := CalculateRuntimeSlotOccupancy(summary, tt.reserved); got != tt.want {
				t.Fatalf("CalculateRuntimeSlotOccupancy() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
