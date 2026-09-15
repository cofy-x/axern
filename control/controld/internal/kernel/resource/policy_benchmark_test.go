package resource

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func BenchmarkAdmissionPolicyEvaluateFit(b *testing.B) {
	policy := AdmissionPolicy{CPUOvercommitRatio: 1.5}
	allocatable := &commonv1.ResourceQuantity{CpuMilli: 64000, MemoryBytes: 256 << 30, EphemeralStorageBytes: 2 << 40}
	used := Claim{CPUMilli: 32000, MemoryBytes: 128 << 30, EphemeralStorageBytes: 1 << 40}
	requested := Claim{CPUMilli: 500, MemoryBytes: 4 << 30, EphemeralStorageBytes: 1 << 30}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !policy.EvaluateFit(allocatable, used, requested).Fits() {
			b.Fatal("benchmark fixture must fit")
		}
	}
}
