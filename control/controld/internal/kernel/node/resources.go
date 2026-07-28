package nodekernel

import (
	"strings"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type RuntimeSlotOccupancy struct {
	Reserved  int64
	Active    int64
	PoolUsing int64
	Occupied  int64
}

// ReportedActiveInstances returns the strongest current runtime occupancy
// signal from a node summary. Pools can move ahead of the component inventory
// during startup, while the component inventory can remain ahead during
// teardown, so placement must conservatively use the maximum.
func ReportedActiveInstances(summary *nodev1.NodeSummary) int64 {
	if summary == nil {
		return 0
	}
	active := int64(len(summary.GetComponents().GetAxnoded().GetActiveAllocationIds()))
	return max(active, int64(summary.GetPools().GetRuntimeSlots().GetUsing()))
}

// CalculateRuntimeSlotOccupancy unions durable reservations with node-reported
// allocation IDs. Counts alone cannot distinguish overlapping ownership from a
// released reservation whose sandbox is still being deleted.
func CalculateRuntimeSlotOccupancy(summary *nodev1.NodeSummary, reservedAllocationIDs []string) RuntimeSlotOccupancy {
	occupiedIDs := allocationIDSet(reservedAllocationIDs)
	reserved := int64(len(occupiedIDs))
	activeIDs := allocationIDSet(summary.GetComponents().GetAxnoded().GetActiveAllocationIds())
	active := int64(len(activeIDs))
	for id := range activeIDs {
		occupiedIDs[id] = struct{}{}
	}
	poolUsing := int64(summary.GetPools().GetRuntimeSlots().GetUsing())
	return RuntimeSlotOccupancy{
		Reserved:  reserved,
		Active:    active,
		PoolUsing: poolUsing,
		Occupied:  max(int64(len(occupiedIDs)), poolUsing),
	}
}

func allocationIDSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = struct{}{}
		}
	}
	return set
}

// RuntimeSlotCapacity returns the node-owned aggregate sandbox slot contract.
// Cgroup and interface pools remain implementation diagnostics and are not
// interpreted by the control plane.
func RuntimeSlotCapacity(summary *nodev1.NodeSummary) (int64, bool) {
	if summary == nil || summary.GetPools() == nil || summary.GetPools().GetRuntimeSlots() == nil {
		return 0, false
	}
	slots := summary.GetPools().GetRuntimeSlots()
	return max(0, int64(slots.GetCapacity()-slots.GetUnavailable())), true
}
