package resource

import (
	"fmt"
	"math"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

const DefaultCPUOvercommitRatio = 1.0
const DefaultRunscRuntimeOverheadMemoryBytes int64 = 256 * 1024 * 1024

type AdmissionPolicy struct {
	CPUOvercommitRatio              float64
	RunscRuntimeOverheadMemoryBytes int64
	DefaultRuntimeName              string
}

type Claim struct {
	CPUMilli              int64
	MemoryBytes           int64
	EphemeralStorageBytes int64
}

type FitEvaluation struct {
	CPU              ResourceEvaluation
	Memory           ResourceEvaluation
	EphemeralStorage ResourceEvaluation
}

type ResourceEvaluation struct {
	Requested            int64
	Used                 int64
	EffectiveAllocatable int64
	Available            int64
	Fits                 bool
}

type NamespaceQuotaPolicy struct {
	CPUMilliLimit              *int64
	MemoryBytesLimit           *int64
	EphemeralStorageBytesLimit *int64
}

type QuotaEvaluation struct {
	CPU              QuotaResourceEvaluation
	Memory           QuotaResourceEvaluation
	EphemeralStorage QuotaResourceEvaluation
}

type QuotaResourceEvaluation struct {
	Requested int64
	Used      int64
	Limit     *int64
	Available *int64
	Fits      bool
}

func NormalizeAdmissionPolicy(policy AdmissionPolicy) AdmissionPolicy {
	if policy.CPUOvercommitRatio == 0 {
		policy.CPUOvercommitRatio = DefaultCPUOvercommitRatio
	}
	if strings.TrimSpace(policy.DefaultRuntimeName) == "" {
		policy.DefaultRuntimeName = "runsc"
	}
	return policy
}

func SaturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}

func ValidateAdmissionPolicy(policy AdmissionPolicy) error {
	if policy.CPUOvercommitRatio <= 0 || math.IsNaN(policy.CPUOvercommitRatio) || math.IsInf(policy.CPUOvercommitRatio, 0) {
		return fmt.Errorf("resource cpu overcommit ratio must be > 0")
	}
	if policy.RunscRuntimeOverheadMemoryBytes < 0 {
		return fmt.Errorf("runsc runtime overhead memory bytes must be >= 0")
	}
	return nil
}

func (p AdmissionPolicy) RuntimeMemoryOverhead(runtimeName string) int64 {
	p = NormalizeAdmissionPolicy(p)
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		runtimeName = p.DefaultRuntimeName
	}
	if runtimeName == "runsc" {
		return p.RunscRuntimeOverheadMemoryBytes
	}
	return 0
}

func (p AdmissionPolicy) EffectiveAllocatable(allocatable *commonv1.ResourceQuantity) Claim {
	p = NormalizeAdmissionPolicy(p)
	return Claim{
		CPUMilli:              scaleCPUMilli(allocatable.GetCpuMilli(), p.CPUOvercommitRatio),
		MemoryBytes:           allocatable.GetMemoryBytes(),
		EphemeralStorageBytes: allocatable.GetEphemeralStorageBytes(),
	}
}

func (p AdmissionPolicy) Fits(allocatable *commonv1.ResourceQuantity, used Claim, requested Claim) bool {
	return p.EvaluateFit(allocatable, used, requested).Fits()
}

func (p AdmissionPolicy) EvaluateFit(allocatable *commonv1.ResourceQuantity, used Claim, requested Claim) FitEvaluation {
	effective := p.EffectiveAllocatable(allocatable)
	return FitEvaluation{
		CPU:              evaluateResource(used.CPUMilli, requested.CPUMilli, effective.CPUMilli),
		Memory:           evaluateResource(used.MemoryBytes, requested.MemoryBytes, effective.MemoryBytes),
		EphemeralStorage: evaluateResource(used.EphemeralStorageBytes, requested.EphemeralStorageBytes, effective.EphemeralStorageBytes),
	}
}

func QuantityToClaim(quantity *commonv1.ResourceQuantity) Claim {
	return Claim{
		CPUMilli:              quantity.GetCpuMilli(),
		MemoryBytes:           quantity.GetMemoryBytes(),
		EphemeralStorageBytes: quantity.GetEphemeralStorageBytes(),
	}
}

func (p NamespaceQuotaPolicy) Fits(used Claim, requested Claim) bool {
	return p.EvaluateFit(used, requested).Fits()
}

func (p NamespaceQuotaPolicy) EvaluateFit(used Claim, requested Claim) QuotaEvaluation {
	return QuotaEvaluation{
		CPU:              evaluateQuotaResource(used.CPUMilli, requested.CPUMilli, p.CPUMilliLimit),
		Memory:           evaluateQuotaResource(used.MemoryBytes, requested.MemoryBytes, p.MemoryBytesLimit),
		EphemeralStorage: evaluateQuotaResource(used.EphemeralStorageBytes, requested.EphemeralStorageBytes, p.EphemeralStorageBytesLimit),
	}
}

func scaleCPUMilli(cpuMilli int64, ratio float64) int64 {
	if cpuMilli <= 0 {
		return cpuMilli
	}
	scaled := math.Floor(float64(cpuMilli) * ratio)
	if scaled > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(scaled)
}

func (e FitEvaluation) Fits() bool {
	return e.CPU.Fits && e.Memory.Fits && e.EphemeralStorage.Fits
}

func (e QuotaEvaluation) Fits() bool {
	return e.CPU.Fits && e.Memory.Fits && e.EphemeralStorage.Fits
}

func evaluateResource(used, requested, limit int64) ResourceEvaluation {
	evaluation := ResourceEvaluation{
		Requested:            requested,
		Used:                 used,
		EffectiveAllocatable: limit,
		Available:            availableResource(used, limit),
	}
	evaluation.Fits = !exceeds(used, requested, limit)
	return evaluation
}

func availableResource(used, limit int64) int64 {
	if limit <= 0 || used >= limit {
		return 0
	}
	return limit - used
}

func exceeds(used, requested, limit int64) bool {
	if requested <= 0 {
		return false
	}
	if limit <= 0 {
		return true
	}
	if used >= limit {
		return true
	}
	return requested > limit-used
}

func evaluateQuotaResource(used, requested int64, limit *int64) QuotaResourceEvaluation {
	evaluation := QuotaResourceEvaluation{
		Requested: requested,
		Used:      used,
		Limit:     limit,
		Fits:      true,
	}
	if limit == nil {
		return evaluation
	}
	available := availableResource(used, *limit)
	evaluation.Available = &available
	evaluation.Fits = !exceeds(used, requested, *limit)
	return evaluation
}
