package hostlinux

// CgroupMemoryReclaimResult describes whether proactive cgroup-v2 memory
// reclaim was necessary and available. A requested result includes the
// kernel's EAGAIN response for a partially fulfilled proactive reclaim.
// memory.reclaim is an optional interface; successful cgroup removal remains
// the authoritative cleanup boundary when it is unavailable or incomplete.
type CgroupMemoryReclaimResult uint8

const (
	CgroupMemoryReclaimNotNeeded CgroupMemoryReclaimResult = iota
	CgroupMemoryReclaimRequested
	CgroupMemoryReclaimUnavailable
)
