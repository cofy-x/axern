package resources

import (
	"fmt"
	"os"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
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
	if cgroupLeaseHasMemoryIdentity(lease) {
		if err := verifyPersistedCgroupIdentity(lease, c.cgroupDriver.Mode()); err != nil {
			if os.IsNotExist(err) {
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
		if os.IsNotExist(err) {
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
	cgroup, err := c.cgroupDriver.Load(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load retiring cgroup: %w", err)
	}
	processes, err := cgroup.Processes(true)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inventory retiring cgroup processes: %w", err)
	}
	if len(processes) != 0 {
		return fmt.Errorf("retiring cgroup still contains %d process(es)", len(processes))
	}
	if observation.Stat["file_dirty"] != 0 || observation.Stat["file_writeback"] != 0 {
		return fmt.Errorf("retiring cgroup writeback has not converged: dirty=%d writeback=%d", observation.Stat["file_dirty"], observation.Stat["file_writeback"])
	}
	if observation.CurrentBytes > 0 {
		requestedAt := current.GetReclaimRequestedAtUnixNano()
		if requestedAt == 0 {
			if err := hostlinux.ReclaimCgroupMemory(id); err != nil {
				metrics.RecordCgroupRetirement("reclaim", "failed")
				return fmt.Errorf("reclaim retiring cgroup memory: %w", err)
			}
			c.Lock()
			current, currentOK = c.leases.Get(id)
			if !currentOK || current == nil || current.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
				c.Unlock()
				return fmt.Errorf("cgroup %s retirement ownership changed after reclaim", id)
			}
			current.CurrentChargedBytes = observation.CurrentBytes
			current.ReclaimRequestedAtUnixNano = time.Now().UTC().UnixNano()
			c.leases.Set(id, current)
			storeErr := c.storeLocked()
			c.Unlock()
			if storeErr != nil {
				return fmt.Errorf("persist retiring cgroup reclaim request: %w", storeErr)
			}
			metrics.RecordCgroupRetirement("reclaim", "requested")
			return fmt.Errorf("retiring cgroup reclaim requested for %d charged bytes", observation.CurrentBytes)
		}
		// memory.current may retain kernel metadata or pages already reparentable
		// at rmdir. Once the explicit reclaim request has had a retry interval and
		// dirty/writeback are zero, deletion is the convergence operation; waiting
		// for an exact zero can strand cleanup debt forever.
		if time.Since(time.Unix(0, requestedAt)) < time.Second {
			return fmt.Errorf("retiring cgroup reclaim is still settling at %d charged bytes", observation.CurrentBytes)
		}
	}
	if err := c.removeCgroupFromSystem(id); err != nil {
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
	c.generator.ReleaseId(id)
	c.Unlock()
	recordPoolState(c)
	return nil
}

func (c *CgroupManager) removeCgroupFromSystem(name string) error {
	err := c.cgroupDriver.Remove(name)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete cgroup %s: %w", name, err)
	}
	return nil
}
