package resource

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestAdmissionPolicyEffectiveAllocatable(t *testing.T) {
	policy := AdmissionPolicy{CPUOvercommitRatio: 2.5}
	effective := policy.EffectiveAllocatable(&commonv1.ResourceQuantity{
		CpuMilli:    1001,
		MemoryBytes: 1024,
	})
	if effective.CPUMilli != 2502 {
		t.Fatalf("cpu = %d, want 2502", effective.CPUMilli)
	}
	if effective.MemoryBytes != 1024 {
		t.Fatalf("memory = %d, want 1024", effective.MemoryBytes)
	}
}

func TestAdmissionPolicyFits(t *testing.T) {
	policy := AdmissionPolicy{CPUOvercommitRatio: 2}
	allocatable := &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1024}
	if !policy.Fits(allocatable, Claim{CPUMilli: 1500, MemoryBytes: 900}, Claim{CPUMilli: 400, MemoryBytes: 100}) {
		t.Fatal("expected request to fit")
	}
	if policy.Fits(allocatable, Claim{CPUMilli: 1500}, Claim{CPUMilli: 600}) {
		t.Fatal("expected cpu request to be rejected")
	}
	if policy.Fits(allocatable, Claim{MemoryBytes: 900}, Claim{MemoryBytes: 200}) {
		t.Fatal("expected memory request to be rejected")
	}
	if policy.Fits(allocatable, Claim{CPUMilli: 2000}, Claim{CPUMilli: 1}) {
		t.Fatal("expected saturated cpu usage to be rejected")
	}
	if policy.Fits(&commonv1.ResourceQuantity{}, Claim{}, Claim{CPUMilli: 1}) {
		t.Fatal("expected zero cpu allocatable to be rejected")
	}
}

func TestAdmissionPolicyEvaluateFit(t *testing.T) {
	policy := AdmissionPolicy{CPUOvercommitRatio: 2}
	evaluation := policy.EvaluateFit(
		&commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 512 << 20},
		Claim{CPUMilli: 1800, MemoryBytes: 400 << 20},
		Claim{CPUMilli: 300, MemoryBytes: 200 << 20},
	)
	if evaluation.Fits() {
		t.Fatal("expected evaluation not to fit")
	}
	if evaluation.CPU.Fits {
		t.Fatal("expected cpu not to fit")
	}
	if evaluation.CPU.Requested != 300 || evaluation.CPU.Used != 1800 || evaluation.CPU.EffectiveAllocatable != 2000 || evaluation.CPU.Available != 200 {
		t.Fatalf("cpu evaluation = %+v, want requested=300 used=1800 effective=2000 available=200", evaluation.CPU)
	}
	if evaluation.Memory.Fits {
		t.Fatal("expected memory not to fit")
	}
	if evaluation.Memory.Requested != 200<<20 || evaluation.Memory.Used != 400<<20 || evaluation.Memory.EffectiveAllocatable != 512<<20 || evaluation.Memory.Available != 112<<20 {
		t.Fatalf("memory evaluation = %+v, want requested=200MiB used=400MiB effective=512MiB available=112MiB", evaluation.Memory)
	}
}

func TestValidateAdmissionPolicy(t *testing.T) {
	for _, ratio := range []float64{1, 2} {
		if err := ValidateAdmissionPolicy(AdmissionPolicy{CPUOvercommitRatio: ratio}); err != nil {
			t.Fatalf("ratio %v returned error: %v", ratio, err)
		}
	}
	for _, ratio := range []float64{0, -1} {
		if err := ValidateAdmissionPolicy(AdmissionPolicy{CPUOvercommitRatio: ratio}); err == nil {
			t.Fatalf("ratio %v returned nil error", ratio)
		}
	}
}

func TestNamespaceQuotaPolicyEvaluateFit(t *testing.T) {
	cpuLimit := int64(1000)
	policy := NamespaceQuotaPolicy{CPUMilliLimit: &cpuLimit}

	if !policy.Fits(Claim{CPUMilli: 800, MemoryBytes: 1 << 30}, Claim{CPUMilli: 200, MemoryBytes: 4 << 30}) {
		t.Fatal("expected request to fit bounded cpu and unbounded memory quota")
	}

	evaluation := policy.EvaluateFit(Claim{CPUMilli: 800}, Claim{CPUMilli: 201})
	if evaluation.Fits() {
		t.Fatal("expected quota evaluation not to fit")
	}
	if evaluation.CPU.Limit == nil || *evaluation.CPU.Limit != 1000 {
		t.Fatalf("cpu limit = %v, want 1000", evaluation.CPU.Limit)
	}
	if evaluation.CPU.Available == nil || *evaluation.CPU.Available != 200 {
		t.Fatalf("cpu available = %v, want 200", evaluation.CPU.Available)
	}
	if !evaluation.Memory.Fits || evaluation.Memory.Limit != nil || evaluation.Memory.Available != nil {
		t.Fatalf("memory evaluation = %+v, want unlimited fit", evaluation.Memory)
	}
}
