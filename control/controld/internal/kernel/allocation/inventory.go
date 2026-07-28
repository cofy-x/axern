package allocationkernel

import (
	"strings"
	"time"
)

const (
	MissingFromNodeInventoryMessage = "allocation missing from node inventory"
	NodeUnavailableMessage          = "node heartbeat stale"
)

type NodeInventorySnapshot struct {
	NodeID              string
	ActiveAllocationIDs []string
	CollectedAt         time.Time
}

type NodeInventoryExpectation struct {
	AllocationID string
	Attempt      int64
	NodeActiveAt time.Time
}

type NodeAllocationRef struct {
	AllocationID string
	Attempt      int64
}

func ExpectedInNodeInventoryAt(nodeActiveAt, snapshotAt time.Time) bool {
	if nodeActiveAt.IsZero() || snapshotAt.IsZero() {
		return false
	}
	return !nodeActiveAt.After(snapshotAt)
}

func MissingFromNodeInventory(snapshot NodeInventorySnapshot, expected []NodeInventoryExpectation) []NodeInventoryExpectation {
	if len(expected) == 0 {
		return nil
	}
	active := make(map[string]struct{}, len(snapshot.ActiveAllocationIDs))
	for _, allocationID := range snapshot.ActiveAllocationIDs {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			continue
		}
		active[allocationID] = struct{}{}
	}
	missing := make([]NodeInventoryExpectation, 0)
	for _, allocation := range expected {
		if _, ok := active[allocation.AllocationID]; ok {
			continue
		}
		if !ExpectedInNodeInventoryAt(allocation.NodeActiveAt, snapshot.CollectedAt) {
			continue
		}
		missing = append(missing, allocation)
	}
	return missing
}
