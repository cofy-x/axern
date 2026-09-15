package placementkernel

import "testing"

func BenchmarkEvaluationLess(b *testing.B) {
	left := &Evaluation{NodeID: "node-a", Rank: &Rank{IdlePoolReady: true, RuntimeSlotOccupancy: 8, ChargedCPUMilli: 4000, ChargedMemoryBytes: 32 << 30, ChargedEphemeralBytes: 8 << 30}}
	right := &Evaluation{NodeID: "node-b", Rank: &Rank{IdlePoolReady: true, RuntimeSlotOccupancy: 9, ChargedCPUMilli: 4500, ChargedMemoryBytes: 36 << 30, ChargedEphemeralBytes: 9 << 30}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !EvaluationLess(left, right) {
			b.Fatal("benchmark fixture order changed")
		}
	}
}
