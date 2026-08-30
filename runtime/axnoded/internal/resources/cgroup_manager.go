package resources

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type CgroupManager struct {
	size      int
	cacheSize int
	rootName  string
	// conformanceRoot is a sibling of rootName below the delegated cgroup-v2
	// root. Node-owned destructive certification runs only in this bounded
	// domain and is excluded from workload slots and memory commitment.
	conformanceRoot string

	poolController *poolController

	usingID cmap.ConcurrentMap[string, struct{}]
	idleID  *queue.Queue[string]
	leases  cmap.ConcurrentMap[string, *apipb.CgroupLease]

	// cgroups tracks every allocation and warm object created under the sandbox
	// root. Assigned objects are one-use: after retirement their IDs are released
	// only after the kernel object and durable lease have both disappeared.
	cgroups              cmap.ConcurrentMap[string, struct{}]
	generator            truncindex.UniqueIdGenerator
	conformanceGenerator truncindex.UniqueIdGenerator
	sync.Mutex
	db stateStore

	// storeMark is used to mark whether the cgroup id need to be stored.
	// If it's true, manager should not exit.
	storeMark atomic.Bool
	storeStop chan struct{}
	storeDone chan struct{}
	storeOnce sync.Once

	gcQueue *queue.Queue[string]
	gcStop  chan struct{}
	gcDone  chan struct{}
	gcOnce  sync.Once

	// cgroupDriver abstracts host cgroup operations for platform implementations and tests.
	cgroupDriver os2.CgroupDriver
	// retirementMemory owns the kernel reads required before assigned memory is
	// converted into durable cleanup debt. Keeping this boundary injectable lets
	// the transition be tested without weakening the production identity checks.
	retirementMemory cgroupRetirementMemory

	memoryCapacity MemoryCapacitySnapshot
	// memoryCapacityIdentity is the last accepted node memory-capacity identity.
	// It is persisted with the lease ledger and deliberately survives sample
	// invalidation. memoryIdentityConflict remains sticky until every commitment
	// from the old identity has completed retirement.
	memoryCapacityIdentity string
	memoryIdentityConflict bool
	// memoryAdmissionRequired makes a fresh, healthy node memory budget a
	// prerequisite for every create, including sandboxes with a zero request.
	// This closes direct-node and report/create races while system reserve is
	// exhausted.
	memoryAdmissionRequired bool
}

type cgroupRetirementMemory interface {
	InspectParent(cgroupPath string) (*hostlinux.CgroupMemoryDomain, error)
	ReadObservation(cgroupPath string) (*hostlinux.CgroupMemoryObservation, error)
}

type hostCgroupRetirementMemory struct{}

func (hostCgroupRetirementMemory) InspectParent(cgroupPath string) (*hostlinux.CgroupMemoryDomain, error) {
	return hostlinux.InspectCgroupMemoryParent(cgroupPath)
}

func (hostCgroupRetirementMemory) ReadObservation(cgroupPath string) (*hostlinux.CgroupMemoryObservation, error) {
	return hostlinux.ReadCgroupMemoryObservation(cgroupPath)
}

func (c *CgroupManager) UpdateMemoryCapacity(snapshot MemoryCapacitySnapshot) error {
	if snapshot.Unavailable {
		c.Lock()
		c.memoryCapacity = MemoryCapacitySnapshot{}
		c.Unlock()
		return nil
	}
	var validationErr error
	if snapshot.EffectiveAllocatableBytes <= 0 {
		validationErr = fmt.Errorf("effective sandbox memory capacity must be positive")
	}
	if validationErr == nil && snapshot.CapacityIdentity == "" {
		validationErr = fmt.Errorf("memory capacity identity is required")
	}
	if validationErr == nil && (snapshot.CapacityIdentity != strings.TrimSpace(snapshot.CapacityIdentity) ||
		!utf8.ValidString(snapshot.CapacityIdentity) || len(snapshot.CapacityIdentity) > 1024) {
		validationErr = fmt.Errorf("memory capacity identity is invalid or exceeds 1024 bytes")
	}
	if validationErr == nil && snapshot.SandboxCurrentBytes < 0 {
		validationErr = fmt.Errorf("sandbox current memory cannot be negative")
	}
	if validationErr == nil && snapshot.SystemReserveAvailableBytes < 0 {
		validationErr = fmt.Errorf("system reserve available memory cannot be negative")
	}
	if validationErr == nil && snapshot.SystemReserveBaseAvailableBytes < snapshot.SystemReserveAvailableBytes {
		validationErr = fmt.Errorf("system reserve base headroom cannot be below current headroom")
	}
	if validationErr == nil && snapshot.SampledAt.IsZero() {
		validationErr = fmt.Errorf("memory capacity sample time is required")
	}
	c.Lock()
	if validationErr != nil {
		c.memoryCapacity = MemoryCapacitySnapshot{}
		c.Unlock()
		return validationErr
	}
	hasCommitment := c.hasCommittedMemoryLeaseLocked()
	if c.memoryIdentityConflict && hasCommitment {
		c.memoryCapacity = MemoryCapacitySnapshot{}
		c.Unlock()
		return fmt.Errorf("node memory capacity identity conflict remains until all allocation commitments retire")
	}
	if !hasCommitment {
		c.memoryIdentityConflict = false
	}
	previousIdentity := c.memoryCapacityIdentity
	if previousIdentity != "" && previousIdentity != snapshot.CapacityIdentity && hasCommitment {
		c.memoryCapacity = MemoryCapacitySnapshot{}
		c.memoryIdentityConflict = true
		c.Unlock()
		return fmt.Errorf("node memory capacity identity changed while allocation commitments remain")
	}
	identityChanged := previousIdentity != snapshot.CapacityIdentity
	previousCapacity := c.memoryCapacity
	previousConflict := c.memoryIdentityConflict
	c.memoryCapacityIdentity = snapshot.CapacityIdentity
	c.memoryIdentityConflict = false
	c.memoryCapacity = snapshot
	if identityChanged {
		if err := c.storeLocked(); err != nil {
			c.memoryCapacity = previousCapacity
			c.memoryCapacityIdentity = previousIdentity
			c.memoryIdentityConflict = previousConflict
			c.Unlock()
			return fmt.Errorf("persist node memory capacity identity: %w", err)
		}
	}
	c.Unlock()
	return nil
}

func (c *CgroupManager) hasCommittedMemoryLeaseLocked() bool {
	for item := range c.leases.IterBuffered() {
		if item.Val == nil {
			continue
		}
		if item.Val.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED ||
			item.Val.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
			return true
		}
	}
	return false
}

func (c *CgroupManager) memoryCommitmentLocked(now time.Time) MemoryCommitment {
	result := MemoryCommitment{}
	for item := range c.leases.IterBuffered() {
		lease := item.Val
		if lease == nil {
			continue
		}
		charge := lease.GetMemoryRequestBytes()
		if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING && lease.GetCurrentChargedBytes() > charge {
			charge = lease.GetCurrentChargedBytes()
		}
		if lease.GetOwnerKind() == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
			switch lease.GetState() {
			case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED:
				result.ConformanceBytes = saturatingMemoryAdd(result.ConformanceBytes, charge)
			case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING:
				result.ConformanceBytes = saturatingMemoryAdd(result.ConformanceBytes, charge)
				result.ConformanceCleanupDebtBytes = saturatingMemoryAdd(result.ConformanceCleanupDebtBytes, charge)
			}
			continue
		}
		switch lease.GetState() {
		case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED:
			result.CommittedBytes = saturatingMemoryAdd(result.CommittedBytes, charge)
		case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING:
			result.CommittedBytes = saturatingMemoryAdd(result.CommittedBytes, charge)
			result.CleanupDebtBytes = saturatingMemoryAdd(result.CleanupDebtBytes, charge)
			result.RetiringCgroupCount++
			if retiringAt := lease.GetRetiringAtUnixNano(); retiringAt > 0 {
				age := now.Sub(time.Unix(0, retiringAt))
				if age > result.OldestRetiringAge {
					result.OldestRetiringAge = age
				}
			}
		}
	}
	return result
}

func saturatingMemoryAdd(current, delta int64) int64 {
	if delta <= 0 {
		return current
	}
	if current > math.MaxInt64-delta {
		return math.MaxInt64
	}
	return current + delta
}

const RetryGenIdTimes = 100

func (c *CgroupManager) MaxSizeLimit() int {
	if c.size == 0 {
		return config.DefaultMaxContainerNum
	}
	return c.size
}

func (c *CgroupManager) CacheSizeLimit() int {
	return c.cacheSize
}

func (c *CgroupManager) UsingNum() int {
	c.Lock()
	defer c.Unlock()
	return c.usingID.Count()
}

func (c *CgroupManager) ShutDown() error {
	if c.poolController != nil {
		c.poolController.shutdown()
	}
	c.gcOnce.Do(func() { close(c.gcStop) })
	<-c.gcDone
	c.stopStoreLoop()
	if err := c.store(); err != nil {
		return fmt.Errorf("persist cgroup ledger during shutdown: %w", err)
	}
	return nil
}

func (c *CgroupManager) Status() ([]string, []string) {
	c.Lock()
	defer c.Unlock()
	return c.usingID.Keys(), c.idleID.List()
}

func (c *CgroupManager) MaxSize() int {
	if c.size == 0 {
		return config.DefaultMaxContainerNum
	}
	return c.size
}

func (c *CgroupManager) ResourceName() ResourceName {
	return CgroupResourceName
}

func (c *CgroupManager) CacheNum() int {
	return c.idleID.Length()
}

func (c *CgroupManager) UnavailableNum() int {
	c.Lock()
	defer c.Unlock()
	count := 0
	for item := range c.leases.IterBuffered() {
		if item.Val != nil && item.Val.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
			count++
		}
	}
	return count
}

func (c *CgroupManager) MemoryCommitment() MemoryCommitment {
	c.Lock()
	defer c.Unlock()
	return c.memoryCommitmentLocked(time.Now().UTC())
}

// AllocationLeaseCleanupStatus reports whether allocationID still owns a
// durable cgroup lease and, when GC has attempted retirement, the latest
// bounded kernel cleanup error. The diagnostic is intentionally node-local:
// durable lease ownership remains the admission authority, while callers that
// wait for convergence need enough evidence to distinguish a live process,
// identity mismatch, and filesystem removal failure. Assigned and retiring
// leases both count: callers must not treat memory capacity as released until
// the retiring lease is removed.
func (c *CgroupManager) AllocationLeaseCleanupStatus(allocationID string) (bool, string) {
	if allocationID == "" {
		return false, ""
	}
	c.Lock()
	defer c.Unlock()
	for item := range c.leases.IterBuffered() {
		if item.Val != nil && item.Val.GetAllocationID() == allocationID {
			return true, item.Val.GetLastCleanupError()
		}
	}
	return false, ""
}

func (c *CgroupManager) setPoolController(controller *poolController) {
	c.poolController = controller
	recordPoolState(c)
}

func (c *CgroupManager) requestPoolRefill(trigger string) {
	if c.poolController != nil {
		c.poolController.request(trigger)
	}
}

var _ Manager = &CgroupManager{}
