package hostlinux

// CgroupMemoryReclaimResult describes whether proactive cgroup-v2 memory
// reclaim was necessary and available. memory.reclaim is an optional kernel
// interface; successful cgroup removal remains the authoritative cleanup
// boundary when it is unavailable.
type CgroupMemoryReclaimResult uint8

const (
	CgroupMemoryReclaimNotNeeded CgroupMemoryReclaimResult = iota
	CgroupMemoryReclaimRequested
	CgroupMemoryReclaimUnavailable
)
