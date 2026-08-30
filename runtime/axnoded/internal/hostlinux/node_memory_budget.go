package hostlinux

type NodeMemoryBudgetSample struct {
	PhysicalCapacityBytes    int64
	SourceAllocatableBytes   int64
	DelegatedRootLimitBytes  int64
	DelegatedRootLimitFinite bool
	SystemReserveBytes       int64
	EffectiveAllocatable     int64
	InternalCurrentBytes     int64
	ConformanceCurrentBytes  int64
	ConformanceLimitBytes    int64
	SandboxCurrentBytes      int64
	CapacityIdentity         string
	SystemReserveExhausted   bool
}

func publicDelegatedRootLimit(kernelLimit int64) (int64, bool) {
	if kernelLimit < 0 {
		return 0, false
	}
	return kernelLimit, true
}
