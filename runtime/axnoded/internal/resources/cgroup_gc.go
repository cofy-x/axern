package resources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func (c *CgroupManager) gc() {
	defer close(c.gcDone)
	for {
		select {
		case <-c.gcStop:
			return
		default:
		}
		metrics.RecordGcQueueLength(string(CgroupResourceName), float64(c.gcQueue.Length()))
		id := c.gcQueue.Pop()
		if id == "" {
			if !c.waitForGCRetry(time.Second) {
				return
			}
			continue
		}
		if err := c.convergeRetiringCgroup(id); err != nil {
			metrics.RecordCgroupRetirement("cleanup", "retry")
			c.recordRetiringFailure(id, err)
			c.gcQueue.Push(id)
			if !c.waitForGCRetry(time.Second) {
				return
			}
			continue
		}
		if err := c.completeRetiringCgroup(id); err != nil {
			metrics.RecordCgroupRetirement("ledger", "retry")
			c.recordRetiringFailure(id, err)
			c.gcQueue.Push(id)
			if !c.waitForGCRetry(time.Second) {
				return
			}
			continue
		}
		metrics.RecordCgroupRetirement("cleanup", "complete")
	}
}

func (c *CgroupManager) waitForGCRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.gcStop:
		return false
	}
}

func (c *CgroupManager) convergeRetiringCgroup(id string) error {
	c.Lock()
	stored, ok := c.leases.Get(id)
	var lease *apipb.CgroupLease
	if ok && stored != nil {
		lease = proto.Clone(stored).(*apipb.CgroupLease)
	}
	c.Unlock()
	if !ok || lease == nil {
		return nil
	}
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		return fmt.Errorf("cgroup %s cleanup requested from state %s", id, lease.GetState())
	}
	staleRoot := c.rootName != "" && !cgroupPathInCurrentRoot(id, c.rootName, c.conformanceRoot)
	if staleRoot && !cgroupLeaseHasMemoryIdentity(lease) {
		if _, err := staleDelegationCgroupProcesses(id, c.ownedRootBase(lease)); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("retiring cgroup %s outside the current delegation has no durable kernel identity", id)
	}
	if cgroupLeaseHasMemoryIdentity(lease) {
		if err := verifyPersistedCgroupIdentity(lease, c.cgroupDriver.Mode()); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("verify retiring cgroup identity: %w", err)
		}
	}
	retirementMemory := c.retirementMemory
	if retirementMemory == nil {
		retirementMemory = hostCgroupRetirementMemory{}
	}
	// Charge the retiring domain before checking process convergence. This is
	// essential for kernel cgroups recovered without a durable owner: an
	// unexplained remaining process must be visible as cleanup debt while GC is
	// deliberately refusing to kill it.
	observation, err := retirementMemory.ReadObservation(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read retiring cgroup memory: %w", err)
	}
	c.Lock()
	current, currentOK := c.leases.Get(id)
	if !currentOK || current == nil || current.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		c.Unlock()
		return fmt.Errorf("cgroup %s retirement ownership changed during cleanup", id)
	}
	current.CurrentChargedBytes = observation.CurrentBytes
	c.leases.Set(id, current)
	c.Unlock()
	processes, err := c.retiringCgroupProcesses(id, staleRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inventory retiring cgroup processes: %w", err)
	}
	if len(processes) != 0 {
		return fmt.Errorf("retiring cgroup still contains %d process(es)", len(processes))
	}
	// memory.current can retain clean page cache and kernel metadata after the
	// allocation has no processes. Do not issue memory.reclaim here: its write is
	// a synchronous, optional kernel operation with no cancellation contract and
	// can block this manager's single durable GC worker indefinitely. Cgroup
	// removal is the authoritative convergence boundary; the kernel reparents
	// remaining charges to the sandbox ancestor, which node-local admission
	// already samples as its current-usage safety floor. A failed removal keeps
	// this lease and its max(request, current charge) cleanup debt for retry.
	if err := c.removeCgroupFromSystem(id, staleRoot); err != nil {
		return err
	}
	return nil
}

func (c *CgroupManager) recordRetiringFailure(id string, cleanupErr error) {
	c.Lock()
	if lease, ok := c.leases.Get(id); ok && lease != nil {
		lease.LastCleanupError = boundedCgroupDiagnostic(cleanupErr.Error())
		c.leases.Set(id, lease)
	}
	if err := c.storeLocked(); err != nil {
		logrus.WithError(err).Error("persist retiring cgroup failure")
	}
	c.Unlock()
}

func (c *CgroupManager) completeRetiringCgroup(id string) error {
	c.Lock()
	lease, ok := c.leases.Get(id)
	if !ok || lease == nil {
		c.Unlock()
		return nil
	}
	lease = proto.Clone(lease).(*apipb.CgroupLease)
	c.leases.Remove(id)
	c.cgroups.Remove(id)
	c.usingID.Remove(id)
	if err := c.storeLocked(); err != nil {
		// The kernel object is already gone, but durable ownership must remain
		// until the ledger records that fact. Restoring the retiring lease keeps
		// the commitment unavailable and makes the next GC pass idempotent.
		c.leases.Set(id, lease)
		c.cgroups.Set(id, struct{}{})
		c.Unlock()
		return fmt.Errorf("persist completed cgroup retirement: %w", err)
	}
	if filepath.Dir(id) == c.conformanceRoot {
		c.conformanceGenerator.ReleaseId(id)
	} else {
		c.generator.ReleaseId(id)
	}
	c.Unlock()
	recordPoolState(c)
	return nil
}

func (c *CgroupManager) retiringCgroupProcesses(name string, staleRoot bool) ([]int, error) {
	if staleRoot {
		lease, _ := c.leases.Get(name)
		return staleDelegationCgroupProcesses(name, c.ownedRootBase(lease))
	}
	cgroup, err := c.cgroupDriver.Load(name)
	if err != nil {
		return nil, err
	}
	return cgroup.Processes(true)
}

func (c *CgroupManager) removeCgroupFromSystem(name string, staleRoot bool) error {
	var err error
	if staleRoot {
		lease, _ := c.leases.Get(name)
		err = removeStaleDelegationCgroup(name, c.ownedRootBase(lease))
	} else {
		err = c.cgroupDriver.Remove(name)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete cgroup %s: %w", name, err)
	}
	return nil
}

func (c *CgroupManager) ownedRootBase(lease *apipb.CgroupLease) string {
	if lease != nil && (lease.GetOwnerKind() == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE ||
		filepath.Base(filepath.Dir(lease.GetCgroupID())) == filepath.Base(c.conformanceRoot)) {
		return filepath.Base(c.conformanceRoot)
	}
	return filepath.Base(c.rootName)
}
