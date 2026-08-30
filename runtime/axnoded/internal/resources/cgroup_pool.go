package resources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

const memoryCapacityFreshness = 15 * time.Second

// Recycle is a one-way assigned -> retiring transition. A cgroup that ever
// belonged to an allocation is never returned to the warm pool.
func (c *CgroupManager) Recycle(id string) error {
	c.Lock()
	lease, ok := c.leases.Get(id)
	if !ok || lease == nil {
		c.Unlock()
		return nil
	}
	if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		c.Unlock()
		return nil
	}
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED {
		c.Unlock()
		return fmt.Errorf("cgroup %s cannot retire from state %s", id, lease.GetState())
	}
	assigned := proto.Clone(lease).(*apipb.CgroupLease)
	retirementMemory := c.retirementMemory
	c.Unlock()

	chargedBytes := int64(0)
	if retirementMemory == nil {
		return fmt.Errorf("cgroup %s retirement memory inspector is unavailable", id)
	}
	domain, err := retirementMemory.InspectParent(id)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect cgroup %s before retirement: %w", id, err)
		}
	} else {
		if cgroupLeaseHasMemoryIdentity(assigned) {
			if err := verifyPersistedCgroupParentIdentity(assigned, domain); err != nil {
				return fmt.Errorf("verify cgroup %s before retirement: %w", id, err)
			}
		}
		observation, err := retirementMemory.ReadObservation(id)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("sample cgroup %s before retirement: %w", id, err)
			}
		} else {
			chargedBytes = observation.CurrentBytes
			if chargedBytes < 0 {
				return fmt.Errorf("sample cgroup %s before retirement: negative memory.current %d", id, chargedBytes)
			}
		}
	}

	c.Lock()
	lease, ok = c.leases.Get(id)
	if !ok || lease == nil {
		c.Unlock()
		return fmt.Errorf("cgroup %s ownership disappeared during retirement", id)
	}
	if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		c.Unlock()
		return nil
	}
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED ||
		lease.GetAllocationID() != assigned.GetAllocationID() || lease.GetAssignedAtUnixNano() != assigned.GetAssignedAtUnixNano() {
		c.Unlock()
		return fmt.Errorf("cgroup %s ownership changed during retirement", id)
	}
	previous := proto.Clone(lease).(*apipb.CgroupLease)
	lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING
	lease.RetiringAtUnixNano = time.Now().UTC().UnixNano()
	lease.CurrentChargedBytes = chargedBytes
	lease.LastCleanupError = ""
	c.leases.Set(id, lease)
	c.usingID.Remove(id)
	if err := c.storeLocked(); err != nil {
		c.leases.Set(id, previous)
		c.usingID.Set(id, struct{}{})
		c.Unlock()
		return err
	}
	c.gcQueue.Push(id)
	c.Unlock()
	recordPoolState(c)
	return nil
}

func (c *CgroupManager) Allocate(opt AllocateOption) (Resource, error) {
	if opt.ContainerID == "" {
		return EmptyStringResource, fmt.Errorf("cgroup allocation requires allocation ownership")
	}
	if opt.MemoryRequestBytes < 0 {
		return EmptyStringResource, fmt.Errorf("cgroup memory request cannot be negative")
	}
	if opt.MemoryLimitBytes < 0 || opt.AllocationAttempt < 0 {
		return EmptyStringResource, fmt.Errorf("cgroup memory limit and allocation attempt cannot be negative")
	}
	if opt.MemoryLimitBytes > 0 && opt.MemoryRequestBytes > opt.MemoryLimitBytes {
		return EmptyStringResource, fmt.Errorf("cgroup memory request cannot exceed its hard limit")
	}
	if opt.CgroupOwnerKind == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED {
		opt.CgroupOwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD
	}
	if opt.CgroupOwnerKind != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD &&
		opt.CgroupOwnerKind != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
		return EmptyStringResource, fmt.Errorf("cgroup allocation owner kind is invalid")
	}

	c.Lock()
	capacity := c.memoryCapacity
	now := time.Now().UTC()
	capacityFresh := !capacity.SampledAt.IsZero() && now.Sub(capacity.SampledAt) <= memoryCapacityFreshness && !capacity.SampledAt.After(now.Add(time.Minute))
	if c.memoryAdmissionRequired {
		if !capacityFresh {
			c.Unlock()
			metrics.RecordMemoryAdmission("capacity_unavailable")
			return EmptyStringResource, fmt.Errorf("node memory capacity observation is unavailable or stale")
		}
		if capacity.SystemReserveExhausted {
			c.Unlock()
			metrics.RecordMemoryAdmission("system_reserve_exhausted")
			return EmptyStringResource, fmt.Errorf("node memory system reserve is exhausted: %w", errord.ErrResourceExhausted)
		}
		if opt.CgroupOwnerKind == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
			if opt.MemoryRequestBytes <= 0 {
				c.Unlock()
				return EmptyStringResource, fmt.Errorf("runtime conformance requires an explicit memory reservation")
			}
			conformanceCommitted := c.memoryCommitmentLocked(now).ConformanceBytes
			commitmentHeadroom := max(capacity.SystemReserveBaseAvailableBytes-conformanceCommitted, 0)
			if opt.MemoryRequestBytes > commitmentHeadroom {
				c.Unlock()
				metrics.RecordMemoryAdmission("conformance_commitment_exhausted")
				return EmptyStringResource, fmt.Errorf(
					"runtime conformance memory reservation %d exceeds system reserve commitment headroom %d: %w",
					opt.MemoryRequestBytes, commitmentHeadroom, errord.ErrResourceExhausted,
				)
			}
			if opt.MemoryRequestBytes > capacity.SystemReserveAvailableBytes {
				c.Unlock()
				metrics.RecordMemoryAdmission("conformance_reserve_exhausted")
				return EmptyStringResource, fmt.Errorf(
					"runtime conformance memory reservation %d exceeds system reserve headroom %d: %w",
					opt.MemoryRequestBytes, capacity.SystemReserveAvailableBytes, errord.ErrResourceExhausted,
				)
			}
		} else if opt.MemoryRequestBytes > 0 {
			committed := c.memoryCommitmentLocked(time.Now().UTC()).CommittedBytes
			if opt.MemoryRequestBytes > capacity.EffectiveAllocatableBytes-committed {
				c.Unlock()
				metrics.RecordMemoryAdmission("commitment_exhausted")
				return EmptyStringResource, fmt.Errorf(
					"sandbox memory request %d exceeds node-local available commitment %d: %w",
					opt.MemoryRequestBytes,
					max(capacity.EffectiveAllocatableBytes-committed, 0),
					errord.ErrResourceExhausted,
				)
			}
			if opt.MemoryRequestBytes > capacity.EffectiveAllocatableBytes-capacity.SandboxCurrentBytes {
				c.Unlock()
				metrics.RecordMemoryAdmission("current_safety_floor")
				return EmptyStringResource, fmt.Errorf(
					"sandbox memory request %d exceeds current safe headroom %d: %w",
					opt.MemoryRequestBytes,
					max(capacity.EffectiveAllocatableBytes-capacity.SandboxCurrentBytes, 0),
					errord.ErrResourceExhausted,
				)
			}
		}
	}
	conformance := opt.CgroupOwnerKind == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE
	if conformance && c.hasActiveConformanceLeaseLocked() {
		c.Unlock()
		metrics.RecordMemoryAdmission("conformance_busy")
		return EmptyStringResource, fmt.Errorf("runtime conformance cgroup is already assigned or retiring: %w", errord.ErrResourceExhausted)
	}
	id := ""
	hit := false
	if !conformance {
		id = c.idleID.Pop()
		hit = id != ""
	}
	if !hit {
		var err error
		id, err = c.createOneLocked(opt.CgroupOwnerKind)
		if err != nil {
			c.Unlock()
			result := ResourcePoolAllocateError
			if errors.Is(err, errord.ErrResourceExhausted) {
				result = ResourcePoolAllocateExhausted
			}
			metrics.RecordResourcePoolAllocate(string(CgroupResourceName), result)
			recordPoolState(c)
			return EmptyStringResource, err
		}
	}
	lease, ok := c.leases.Get(id)
	if !ok || lease == nil || lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE {
		c.Unlock()
		return EmptyStringResource, fmt.Errorf("cgroup %s is not a durable idle lease", id)
	}
	lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED
	lease.AllocationID = opt.ContainerID
	lease.MemoryRequestBytes = opt.MemoryRequestBytes
	lease.MemoryLimitBytes = opt.MemoryLimitBytes
	lease.AllocationAttempt = opt.AllocationAttempt
	lease.RuntimeName = opt.RuntimeName
	lease.OwnerKind = opt.CgroupOwnerKind
	lease.AssignedAtUnixNano = time.Now().UTC().UnixNano()
	c.leases.Set(id, lease)
	c.usingID.Set(id, struct{}{})
	if err := c.storeLocked(); err != nil {
		c.usingID.Remove(id)
		if conformance {
			// This object was created synchronously for one certification run and
			// has never been exposed to a runtime. It cannot become a warm
			// workload object after an ownership persistence failure.
			lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING
			lease.AllocationID = ""
			lease.MemoryRequestBytes = 0
			lease.MemoryLimitBytes = 0
			lease.AllocationAttempt = 0
			lease.RuntimeName = ""
			lease.OwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED
			lease.AssignedAtUnixNano = 0
			lease.RetiringAtUnixNano = time.Now().UTC().UnixNano()
			c.leases.Set(id, lease)
			c.gcQueue.Push(id)
			c.Unlock()
			return EmptyStringResource, err
		}
		lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE
		lease.AllocationID = ""
		lease.MemoryRequestBytes = 0
		lease.MemoryLimitBytes = 0
		lease.AllocationAttempt = 0
		lease.RuntimeName = ""
		lease.OwnerKind = apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED
		lease.AssignedAtUnixNano = 0
		c.leases.Set(id, lease)
		c.idleID.Push(id)
		c.Unlock()
		return EmptyStringResource, err
	}
	c.Unlock()
	if c.memoryAdmissionRequired && opt.MemoryRequestBytes > 0 {
		result := "admitted"
		if conformance {
			result = "conformance_admitted"
		}
		metrics.RecordMemoryAdmission(result)
	}

	if hit {
		metrics.RecordResourcePoolAllocate(string(CgroupResourceName), ResourcePoolAllocateHit)
	} else {
		metrics.RecordResourcePoolAllocate(string(CgroupResourceName), ResourcePoolAllocateMissSyncCreate)
	}
	if !conformance && c.CacheNum() < c.CacheSizeLimit() {
		c.requestPoolRefill(ResourcePoolTriggerLowWatermark)
	}
	recordPoolState(c)
	return NewStringResource(id), nil
}

func (c *CgroupManager) Add(num int) int {
	c.Lock()
	added := c.addLocked(num)
	if added > 0 {
		if err := c.storeLocked(); err != nil {
			logrus.WithError(err).Error("persist warm cgroup pool")
		}
	}
	c.Unlock()
	recordPoolState(c)
	return added
}

func (c *CgroupManager) addLocked(num int) int {
	added := 0
	for i := 0; i < num; i++ {
		id, err := c.createOneLocked(apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD)
		if err != nil {
			logrus.WithError(err).Error("create warm cgroup")
			continue
		}
		c.idleID.Push(id)
		added++
	}
	return added
}

func (c *CgroupManager) Del(num int) {
	c.Lock()
	for i := 0; i < num; i++ {
		id := c.idleID.Pop()
		if id == "" {
			break
		}
		lease, ok := c.leases.Get(id)
		if !ok || lease == nil {
			continue
		}
		lease.State = apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING
		lease.RetiringAtUnixNano = time.Now().UTC().UnixNano()
		c.leases.Set(id, lease)
		c.gcQueue.Push(id)
	}
	if err := c.storeLocked(); err != nil {
		logrus.WithError(err).Error("persist cgroup pool shrink")
	}
	c.Unlock()
	recordPoolState(c)
}

func (c *CgroupManager) createOneLocked(owner apipb.CgroupLeaseOwnerKind) (string, error) {
	if owner != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE && c.workloadCgroupCountLocked() >= c.MaxSizeLimit() {
		return "", errord.ErrResourceExhausted
	}
	generator := c.generator
	if owner == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
		generator = c.conformanceGenerator
	}
	id, err := generator.GetID()
	if err != nil {
		return "", err
	}
	if _, err := c.cgroupDriver.Create(id, &specs.LinuxResources{}); err != nil {
		generator.ReleaseId(id)
		return "", err
	}
	c.cgroups.Set(id, struct{}{})
	c.leases.Set(id, &apipb.CgroupLease{CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE})
	return id, nil
}

func (c *CgroupManager) hasActiveConformanceLeaseLocked() bool {
	for item := range c.leases.IterBuffered() {
		lease := item.Val
		if lease != nil && lease.GetOwnerKind() == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE &&
			(lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED ||
				lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING) {
			return true
		}
	}
	return false
}

func (c *CgroupManager) workloadCgroupCountLocked() int {
	count := 0
	for id := range c.cgroups.Items() {
		lease, _ := c.leases.Get(id)
		if lease != nil && lease.GetOwnerKind() == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
			continue
		}
		// An unowned kernel object discovered below the current conformance
		// domain is certification cleanup debt. Every other unknown or stale
		// object is conservatively charged to workload slot capacity.
		if filepath.Dir(id) == c.conformanceRoot {
			continue
		}
		count++
	}
	return count
}
