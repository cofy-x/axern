package resources

import "time"

type ResourceName string

type PoolStatus struct {
	Using       int
	Idle        int
	Capacity    int
	Unavailable int
}

type MemoryCommitment struct {
	CommittedBytes              int64
	CleanupDebtBytes            int64
	ConformanceBytes            int64
	ConformanceCleanupDebtBytes int64
	RetiringCgroupCount         int
	OldestRetiringAge           time.Duration
}

// MemoryCapacitySnapshot is the latest node-observed local admission boundary.
// The resource manager consumes it under the same lock that persists allocation
// ownership, making capacity check and reservation one atomic operation.
type MemoryCapacitySnapshot struct {
	// Unavailable is an explicit invalidation publication. It clears any prior
	// sample so a recently healthy capacity cannot remain admissible after a
	// mount, boot, resource-source, or collector failure.
	Unavailable                     bool
	EffectiveAllocatableBytes       int64
	SandboxCurrentBytes             int64
	SystemReserveBaseAvailableBytes int64
	SystemReserveAvailableBytes     int64
	SystemReserveExhausted          bool
	CapacityIdentity                string
	SampledAt                       time.Time
}

const (
	CgroupResourceName    ResourceName = "cgroup"
	InterfaceResourceName ResourceName = "interface"

	ResourceAnnotationKeyPrefix = "io.axnoded.resource/"
)

const (
	bridgeName        = "sandbox0"
	containerEthName  = "eth0"
	ContainerLoopName = "lo"
	bridgeMac         = "02:3f:e1:bd:13:b8"

	// maxVethNum is the max number of veth pair,
	// can not exceed 1024  which is  the limitation of netlink for one bridge device.
	maxVethNum = 1000
)

type StringResource string

var (
	emptyString                  = StringResource("")
	EmptyStringResource Resource = &emptyString
)

type Resource interface {
	ToString() string
	FromString(string) error
}

func (s *StringResource) ToString() string {
	return string(*s)
}

func (s *StringResource) FromString(str string) error {
	*s = StringResource(str)
	return nil
}

func NewStringResource(str string) Resource {
	s := StringResource(str)
	return &s
}
