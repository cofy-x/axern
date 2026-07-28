package node

import "testing"

func TestNormalizeInventoryURL(t *testing.T) {
	if got, want := normalizeInventoryURL("127.0.0.1:23001"), "http://127.0.0.1:23001/inventoryz"; got != want {
		t.Fatalf("normalizeInventoryURL = %q, want %q", got, want)
	}
	if got, want := normalizeInventoryURL("https://node.example/inventoryz"), "https://node.example/inventoryz"; got != want {
		t.Fatalf("normalizeInventoryURL = %q, want %q", got, want)
	}
}

func TestResourceRowsExposeCommitmentAndUnboundedCounts(t *testing.T) {
	rows := resourceRows(&inventorySnapshot{
		Node: inventoryNode{Capacity: resourceQuantity{CPUMilli: 4000, MemoryBytes: 8 << 30}},
		Resources: inventoryResources{
			CPU: inventoryCPU{
				AxnodedCommittedMilli: 500,
				AxnodedUsedMilli:      125,
				AxnodedUnboundedCount: 1,
			},
			Memory: inventoryMemory{
				AxnodedCommittedBytes: 4 << 30,
				AxnodedUsedBytes:      128 << 20,
				AxnodedUnboundedCount: 2,
			},
		},
		Components: inventoryComponents{Axnoded: inventoryAxnoded{RunningContainers: 3}},
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Resource != "cpu_milli" || rows[0].Committed != 500 || rows[0].Used != 125 || rows[0].Unbounded != 1 {
		t.Fatalf("unexpected cpu row: %+v", rows[0])
	}
	if rows[1].Resource != "memory_bytes" || rows[1].Committed != 4<<30 || rows[1].Used != 128<<20 || rows[1].Unbounded != 2 {
		t.Fatalf("unexpected memory row: %+v", rows[1])
	}
	if rows[0].RunningCount != 3 || rows[1].RunningCount != 3 {
		t.Fatalf("running counts = %+v, want 3", rows)
	}
}
