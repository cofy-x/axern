package resources

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func NewCgroupManager(db stateStore, cfg config.ResourceConfig, memoryAdmissionRequired bool) (*CgroupManager, error) {
	rootName, err := cfg.CgroupRootNameValue()
	if err != nil {
		return nil, err
	}
	cgroupDriver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return nil, err
	}
	if cgroupDriver.Mode() != os2.CgroupModeV2 {
		return nil, fmt.Errorf("allocation cgroup manager requires unified cgroup v2, got %q", cgroupDriver.Mode())
	}
	resolvedRoot, err := cgroupDriver.ResolveRoot(rootName)
	if err != nil {
		return nil, fmt.Errorf("resolve allocation cgroup root %q: %w", rootName, err)
	}

	var ledger apipb.CgroupLedger
	if err := db.LoadSnapshot(config.CgroupBucket, &ledger); err != nil && !errord.IsNotFound(err) {
		return nil, fmt.Errorf("load cgroup ownership ledger: %w", err)
	}
	if identity := strings.TrimSpace(ledger.GetMemoryCapacityIdentity()); len(identity) > 1024 || !utf8.ValidString(identity) {
		return nil, fmt.Errorf("cgroup ledger memory capacity identity is invalid or exceeds 1024 bytes")
	} else if identity != ledger.GetMemoryCapacityIdentity() {
		return nil, fmt.Errorf("cgroup ledger memory capacity identity is not canonical")
	}
	reconciledLeases, discardedRecreatableLeases, err := reconcileCgroupLeasesForRoot(ledger.GetLeases(), resolvedRoot)
	if err != nil {
		return nil, err
	}
	if discardedRecreatableLeases > 0 {
		logrus.WithFields(logrus.Fields{
			"discarded_recreatable_leases": discardedRecreatableLeases,
			"resolved_root":                resolvedRoot,
		}).Info("discarded recreatable cgroup leases from a previous delegation root")
	}
	if err := cgroupDriver.EnsureRoot(rootName); err != nil {
		return nil, fmt.Errorf("prepare allocation cgroup root %q: %w", rootName, err)
	}
	cgs, err := loadAllCgroups(cgroupDriver, resolvedRoot)
	if err != nil {
		return nil, err
	}

	idleIDs := queue.New("")
	usingIDs := cmap.New[struct{}]()
	leases := cmap.New[*apipb.CgroupLease]()
	gcQueue := queue.New("")
	for _, lease := range reconciledLeases {
		kernelPresent := cgs.Has(lease.GetCgroupID())
		copy := proto.Clone(lease).(*apipb.CgroupLease)
		if !kernelPresent {
			if missingKernelLeaseConverged(lease.GetState()) {
				// An unused warm object can be recreated, and a retiring object that
				// has disappeared has completed its cleanup barrier.
				continue
			}
			// Never release assigned ownership before runtime inventory has been
			// reconciled. Reserving the missing ID prevents reuse; an active
			// allocation will fail its enforcement audit, while an unclaimed
			// lease is transitioned to retiring by ReconcileResourceClaims.
			cgs.Set(lease.GetCgroupID(), struct{}{})
			copy.LastIdentityVerificationError = "assigned cgroup kernel object is missing"
		}
		if kernelPresent && cgroupLeaseHasMemoryIdentity(lease) {
			if err := verifyPersistedCgroupIdentity(lease, cgroupDriver.Mode()); err != nil {
				copy.LastIdentityVerificationError = boundedCgroupDiagnostic(err.Error())
				logrus.WithError(err).WithField("cgroup", lease.GetCgroupID()).Error("durable cgroup identity verification failed; preserving allocation ownership")
			} else {
				copy.LastIdentityVerificationError = ""
			}
		}
		leases.Set(copy.GetCgroupID(), copy)
		switch copy.GetState() {
		case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE:
			idleIDs.Push(copy.GetCgroupID())
		case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED:
			usingIDs.Set(copy.GetCgroupID(), struct{}{})
		case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING:
			gcQueue.Push(copy.GetCgroupID())
		}
	}

	// A kernel cgroup without durable ownership can never become an idle warm
	// object. Preserve it as cleanup debt until the retiring worker proves it is
	// empty and removes it.
	now := time.Now().UTC().UnixNano()
	for id := range cgs.Items() {
		if leases.Has(id) {
			continue
		}
		lease := &apipb.CgroupLease{CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING, RetiringAtUnixNano: now, LastCleanupError: "cgroup has no durable ownership record"}
		leases.Set(id, lease)
		gcQueue.Push(id)
	}

	c := &CgroupManager{
		size: cfg.MaxInstanceNum, cacheSize: cfg.CgroupCacheSize, rootName: resolvedRoot,
		usingID: usingIDs, idleID: idleIDs, leases: leases, gcQueue: gcQueue,
		gcStop: make(chan struct{}), gcDone: make(chan struct{}),
		generator: truncindex.NewFixLenGenerator(12, cgs.Keys(), truncindex.PrefixModifier(resolvedRoot+"/")),
		db:        db, cgroups: cgs, storeMark: atomic.Bool{}, storeStop: make(chan struct{}), storeDone: make(chan struct{}),
		cgroupDriver: cgroupDriver, retirementMemory: hostCgroupRetirementMemory{}, memoryAdmissionRequired: memoryAdmissionRequired,
		memoryCapacityIdentity: ledger.GetMemoryCapacityIdentity(),
	}
	if memoryAdmissionRequired && c.hasCommittedMemoryLeaseLocked() && c.memoryCapacityIdentity == "" {
		return nil, fmt.Errorf("cgroup ledger has allocation commitments without a durable node memory identity")
	}
	if err := c.store(); err != nil {
		return nil, err
	}
	c.keepStoring()
	go c.gc()
	return c, nil
}

func reconcileCgroupLeasesForRoot(leases []*apipb.CgroupLease, resolvedRoot string) ([]*apipb.CgroupLease, int, error) {
	result := make([]*apipb.CgroupLease, 0, len(leases))
	discardedRecreatable := 0
	for _, lease := range leases {
		if err := validateCgroupLease(lease); err != nil {
			return nil, 0, err
		}
		if filepath.Dir(lease.GetCgroupID()) == resolvedRoot {
			result = append(result, lease)
			continue
		}
		if staleCgroupLeaseIsRecreatable(lease) {
			// Never-assigned leases carry no allocation commitment. Internal
			// conformance is node-owned, cannot be requested over RPC, and is
			// retried from a deterministic identity whose preflight removes any
			// remaining runtime/storage artifacts. Recreate both below the
			// current root instead of making a Pod replacement crash-loop.
			discardedRecreatable++
			continue
		}
		return nil, 0, fmt.Errorf(
			"non-idle cgroup %s is outside resolved sandbox root %s; drain allocations and reconcile cleanup debt before replacing the delegated root",
			lease.GetCgroupID(), resolvedRoot,
		)
	}
	return result, discardedRecreatable, nil
}

func staleCgroupLeaseIsRecreatable(lease *apipb.CgroupLease) bool {
	if lease == nil {
		return false
	}
	if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE {
		return true
	}
	if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING &&
		lease.GetAllocationID() == "" {
		return true
	}
	return lease.GetOwnerKind() == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE
}

func missingKernelLeaseConverged(state apipb.CgroupLifecycleState) bool {
	return state == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE ||
		state == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING
}

func validateCgroupLease(lease *apipb.CgroupLease) error {
	if lease == nil || lease.GetCgroupID() == "" {
		return fmt.Errorf("cgroup ledger contains an empty lease")
	}
	switch lease.GetState() {
	case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE:
		if lease.GetAllocationID() != "" || lease.GetMemoryRequestBytes() != 0 || lease.GetMemoryLimitBytes() != 0 ||
			lease.GetAllocationAttempt() != 0 || lease.GetRuntimeName() != "" || lease.GetAssignedAtUnixNano() != 0 ||
			lease.GetRetiringAtUnixNano() != 0 || lease.GetReclaimRequestedAtUnixNano() != 0 || cgroupLeaseHasAnyMemoryIdentity(lease) ||
			lease.GetOwnerKind() != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED {
			return fmt.Errorf("idle cgroup %s contains allocation ownership", lease.GetCgroupID())
		}
	case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED:
		if lease.GetAllocationID() == "" || lease.GetAssignedAtUnixNano() <= 0 || lease.GetRetiringAtUnixNano() != 0 || lease.GetReclaimRequestedAtUnixNano() != 0 ||
			(lease.GetRuntimeName() != "runc" && lease.GetRuntimeName() != "runsc") || !validAssignedCgroupOwner(lease.GetOwnerKind()) {
			return fmt.Errorf("assigned cgroup %s has malformed ownership", lease.GetCgroupID())
		}
	case apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING:
		if lease.GetRetiringAtUnixNano() <= 0 {
			return fmt.Errorf("retiring cgroup %s has no retirement time", lease.GetCgroupID())
		}
		if requestedAt := lease.GetReclaimRequestedAtUnixNano(); requestedAt < 0 || requestedAt > time.Now().Add(time.Minute).UnixNano() {
			return fmt.Errorf("retiring cgroup %s has invalid reclaim request time", lease.GetCgroupID())
		}
		if lease.GetAllocationID() == "" {
			if lease.GetOwnerKind() != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_UNSPECIFIED ||
				lease.GetMemoryRequestBytes() != 0 || lease.GetMemoryLimitBytes() != 0 || lease.GetRuntimeName() != "" ||
				lease.GetAssignedAtUnixNano() != 0 || cgroupLeaseHasAnyMemoryIdentity(lease) {
				return fmt.Errorf("unowned retiring cgroup %s contains allocation ownership", lease.GetCgroupID())
			}
		} else if lease.GetAssignedAtUnixNano() <= 0 || (lease.GetRuntimeName() != "runc" && lease.GetRuntimeName() != "runsc") ||
			!validAssignedCgroupOwner(lease.GetOwnerKind()) {
			return fmt.Errorf("retiring cgroup %s has malformed allocation ownership", lease.GetCgroupID())
		}
	default:
		return fmt.Errorf("cgroup %s has invalid lifecycle state %s", lease.GetCgroupID(), lease.GetState())
	}
	if lease.GetMemoryRequestBytes() < 0 || lease.GetMemoryLimitBytes() < 0 || lease.GetAllocationAttempt() < 0 || lease.GetCurrentChargedBytes() < 0 {
		return fmt.Errorf("cgroup %s has negative memory accounting", lease.GetCgroupID())
	}
	if lease.GetMemoryLimitBytes() > 0 && lease.GetMemoryRequestBytes() > lease.GetMemoryLimitBytes() {
		return fmt.Errorf("cgroup %s memory request exceeds its hard limit", lease.GetCgroupID())
	}
	if cgroupLeaseHasAnyMemoryIdentity(lease) && !cgroupLeaseHasMemoryIdentity(lease) {
		return fmt.Errorf("cgroup %s has a partial memory identity", lease.GetCgroupID())
	}
	if cgroupLeaseHasMemoryIdentity(lease) {
		if lease.GetMemoryLimitBytes() <= 0 {
			return fmt.Errorf("cgroup %s has memory identity without a hard limit", lease.GetCgroupID())
		}
		if len(lease.GetCgroupBootID()) > 128 || len(lease.GetCgroupMountIdentity()) > 1024 || lease.GetCgroupParentInode() == lease.GetCgroupLeafInode() {
			return fmt.Errorf("cgroup %s has malformed memory identity", lease.GetCgroupID())
		}
	}
	if len(lease.GetLastIdentityVerificationError()) > 1024 || !utf8.ValidString(lease.GetLastIdentityVerificationError()) {
		return fmt.Errorf("cgroup %s identity diagnostic is invalid or exceeds 1024 bytes", lease.GetCgroupID())
	}
	if len(lease.GetLastCleanupError()) > 1024 || !utf8.ValidString(lease.GetLastCleanupError()) {
		return fmt.Errorf("cgroup %s cleanup diagnostic is invalid or exceeds 1024 bytes", lease.GetCgroupID())
	}
	return nil
}

func validAssignedCgroupOwner(owner apipb.CgroupLeaseOwnerKind) bool {
	return owner == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD ||
		owner == apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE
}

func boundedCgroupDiagnostic(message string) string {
	const maxBytes = 1024
	message = strings.ToValidUTF8(message, "")
	if len(message) <= maxBytes {
		return message
	}
	message = message[:maxBytes]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func cgroupLeaseHasAnyMemoryIdentity(lease *apipb.CgroupLease) bool {
	return lease != nil && (lease.GetCgroupBootID() != "" || lease.GetCgroupMountIdentity() != "" ||
		lease.GetCgroupParentInode() != 0 || lease.GetCgroupLeafInode() != 0)
}

func cgroupLeaseHasMemoryIdentity(lease *apipb.CgroupLease) bool {
	return lease != nil && lease.GetCgroupBootID() != "" && lease.GetCgroupMountIdentity() != "" &&
		lease.GetCgroupParentInode() != 0 && lease.GetCgroupLeafInode() != 0
}

func verifyPersistedCgroupIdentity(lease *apipb.CgroupLease, mode string) error {
	if lease == nil || !cgroupLeaseHasMemoryIdentity(lease) {
		return fmt.Errorf("complete cgroup memory identity is required")
	}
	var (
		domain *hostlinux.CgroupMemoryDomain
		err    error
	)
	if lease.GetState() == apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING {
		domain, err = hostlinux.InspectCgroupMemoryParent(lease.GetCgroupID())
	} else {
		domain, err = hostlinux.InspectCgroupMemoryDomain(lease.GetCgroupID(), os2.WorkloadGroup(lease.GetCgroupID(), mode))
	}
	if err != nil {
		return err
	}
	if err := verifyPersistedCgroupParentIdentity(lease, domain); err != nil {
		return err
	}
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING && domain.LeafInode != lease.GetCgroupLeafInode() {
		return fmt.Errorf("workload cgroup identity changed")
	}
	return nil
}

func verifyPersistedCgroupParentIdentity(lease *apipb.CgroupLease, domain *hostlinux.CgroupMemoryDomain) error {
	if lease == nil || domain == nil || !cgroupLeaseHasMemoryIdentity(lease) {
		return fmt.Errorf("complete cgroup memory identity is required")
	}
	if domain.BootID != lease.GetCgroupBootID() || domain.MountIdentity != lease.GetCgroupMountIdentity() ||
		domain.ParentInode != lease.GetCgroupParentInode() {
		return fmt.Errorf("allocation cgroup identity changed")
	}
	if domain.LimitBytes != lease.GetMemoryLimitBytes() || domain.SwapMaxBytes != 0 || !domain.OOMGroup {
		return fmt.Errorf("sandbox memory controls changed: limit=%d swap=%d oom_group=%t", domain.LimitBytes, domain.SwapMaxBytes, domain.OOMGroup)
	}
	return nil
}

// BindMemoryDomain atomically attaches the post-create kernel identity to the
// allocation lease. It is idempotent only for the exact same identity; a
// different identity is never allowed to replace durable ownership.
func (c *CgroupManager) BindMemoryDomain(cgroupID, allocationID string, limitBytes int64, bootID, mountIdentity string, parentInode, leafInode uint64) error {
	if cgroupID == "" || allocationID == "" || limitBytes <= 0 || bootID == "" || mountIdentity == "" ||
		parentInode == 0 || leafInode == 0 || parentInode == leafInode {
		return fmt.Errorf("complete allocation cgroup memory identity is required")
	}
	c.Lock()
	defer c.Unlock()
	lease, ok := c.leases.Get(cgroupID)
	if !ok || lease == nil || lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED ||
		lease.GetAllocationID() != allocationID || lease.GetMemoryLimitBytes() != limitBytes {
		return fmt.Errorf("cgroup %s is not the matching assigned allocation lease", cgroupID)
	}
	if cgroupLeaseHasAnyMemoryIdentity(lease) {
		if lease.GetCgroupBootID() == bootID && lease.GetCgroupMountIdentity() == mountIdentity &&
			lease.GetCgroupParentInode() == parentInode && lease.GetCgroupLeafInode() == leafInode {
			if lease.GetLastIdentityVerificationError() == "" {
				return nil
			}
			previous := proto.Clone(lease).(*apipb.CgroupLease)
			lease.LastIdentityVerificationError = ""
			c.leases.Set(cgroupID, lease)
			if err := c.storeLocked(); err != nil {
				c.leases.Set(cgroupID, previous)
				return fmt.Errorf("clear allocation cgroup identity diagnostic: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cgroup %s is already bound to a different kernel identity", cgroupID)
	}
	previous := proto.Clone(lease).(*apipb.CgroupLease)
	lease.CgroupBootID = bootID
	lease.CgroupMountIdentity = mountIdentity
	lease.CgroupParentInode = parentInode
	lease.CgroupLeafInode = leafInode
	lease.LastIdentityVerificationError = ""
	c.leases.Set(cgroupID, lease)
	if err := c.storeLocked(); err != nil {
		c.leases.Set(cgroupID, previous)
		return fmt.Errorf("persist allocation cgroup memory identity: %w", err)
	}
	return nil
}

func (c *CgroupManager) RetiringMemoryLeases() []RetiringMemoryLease {
	c.Lock()
	defer c.Unlock()
	result := make([]RetiringMemoryLease, 0)
	for item := range c.leases.IterBuffered() {
		lease := item.Val
		if lease == nil || lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING ||
			lease.GetAllocationID() == "" || lease.GetAllocationAttempt() <= 0 {
			continue
		}
		result = append(result, RetiringMemoryLease{
			CgroupID: lease.GetCgroupID(), AllocationID: lease.GetAllocationID(), AllocationAttempt: lease.GetAllocationAttempt(),
			MemoryRequest: lease.GetMemoryRequestBytes(), MemoryLimit: lease.GetMemoryLimitBytes(), RuntimeName: lease.GetRuntimeName(),
			BootID: lease.GetCgroupBootID(), MountIdentity: lease.GetCgroupMountIdentity(),
			ParentInode: lease.GetCgroupParentInode(), LeafInode: lease.GetCgroupLeafInode(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AllocationID < result[j].AllocationID })
	return result
}

func (c *CgroupManager) keepStoring() {
	go func() {
		defer close(c.storeDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.storeMark.Load() {
					if err := c.store(); err != nil {
						logrus.WithError(err).Error("persist cgroup ownership ledger")
					}
				}
			case <-c.storeStop:
				return
			}
		}
	}()
}

func (c *CgroupManager) stopStoreLoop() {
	if c.storeStop == nil || c.storeDone == nil {
		return
	}
	c.storeOnce.Do(func() { close(c.storeStop) })
	<-c.storeDone
}

func (c *CgroupManager) store() error {
	c.Lock()
	defer c.Unlock()
	return c.storeLocked()
}

func (c *CgroupManager) storeLocked() error {
	ids := c.leases.Keys()
	sort.Strings(ids)
	ledger := &apipb.CgroupLedger{
		Leases:                 make([]*apipb.CgroupLease, 0, len(ids)),
		MemoryCapacityIdentity: c.memoryCapacityIdentity,
	}
	for _, id := range ids {
		lease, ok := c.leases.Get(id)
		if !ok || lease == nil {
			continue
		}
		ledger.Leases = append(ledger.Leases, proto.Clone(lease).(*apipb.CgroupLease))
	}
	if err := c.db.SaveSnapshot(config.CgroupBucket, ledger); err != nil {
		c.storeMark.Store(true)
		return fmt.Errorf("save cgroup ownership ledger: %w", err)
	}
	c.storeMark.Store(false)
	return nil
}

func loadAllCgroups(driver os2.CgroupDriver, rootName string) (cmap.ConcurrentMap[string, struct{}], error) {
	groupDirs, err := driver.ExistingGroups(rootName)
	if err != nil {
		return cmap.New[struct{}](), err
	}
	cgroups := cmap.New[struct{}]()
	for _, dir := range groupDirs {
		cgroups.Set(dir, struct{}{})
	}
	return cgroups, nil
}
