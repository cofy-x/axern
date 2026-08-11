package nodeinventory

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	allocationMemoryMetricRuntimes = []string{"runc", "runsc"}
	allocationMemoryMetricKinds    = []string{
		"current", "peak", "swap_current", "anon", "file", "shmem", "kernel", "file_dirty", "file_writeback",
		"event_high", "event_max", "event_oom", "event_oom_kill", "event_oom_group_kill",
		"psi_some_total_usec", "psi_full_total_usec", "psi_some_avg10", "psi_full_avg10", "psi_available_allocations",
	}
)

func (s *AxnodedSource) collectAxnodedInventory(now time.Time, snapshot *NodeInventorySnapshot) bool {
	allContainers := s.container.List()
	runningContainers := make([]*container.Container, 0)
	for _, c := range allContainers {
		if c == nil || c.Status == nil {
			continue
		}
		if c.Status.Get().State() == runtimeapi.ContainerState_CONTAINER_RUNNING {
			runningContainers = append(runningContainers, c)
		}
	}

	if s.ready != nil {
		snapshot.Components.Axnoded.Ready = s.ready()
	}
	snapshot.Components.Axnoded.RunningContainers = len(runningContainers)
	snapshot.Components.Axnoded.RunningAllocationIDs = make([]string, 0, len(runningContainers))
	snapshot.Components.Axnoded.ActiveAllocationIDs = make([]string, 0, len(allContainers))
	if s.runtimeCount != nil {
		snapshot.Components.Axnoded.RegisteredRuntimes = s.runtimeCount()
	}
	if s.langRuntime != nil {
		retentionStats := s.langRuntime.RetentionStats()
		snapshot.Heat.RetainedRuntimeCount = retentionStats.RetainedRuntimeCount
		snapshot.Heat.RetainedRootfsCount = retentionStats.RetainedRootfsCount
		s.collectAxnodedLocality(snapshot, runningContainers)
	}

	for _, c := range allContainers {
		if c == nil || c.Metadata == nil {
			continue
		}
		allocationID := strings.TrimSpace(c.Metadata.ID)
		if allocationID == "" {
			continue
		}
		if c.Status == nil {
			snapshot.Components.Axnoded.ActiveAllocationIDs = append(snapshot.Components.Axnoded.ActiveAllocationIDs, allocationID)
			continue
		}
		switch c.Status.Get().State() {
		case runtimeapi.ContainerState_CONTAINER_EXITED:
			continue
		case runtimeapi.ContainerState_CONTAINER_RUNNING:
			snapshot.Components.Axnoded.RunningAllocationIDs = append(snapshot.Components.Axnoded.RunningAllocationIDs, allocationID)
		}
		snapshot.Components.Axnoded.ActiveAllocationIDs = append(snapshot.Components.Axnoded.ActiveAllocationIDs, allocationID)
	}
	for _, c := range runningContainers {
		status := c.Status.Get()
		res := status.LinuxResources
		if committedMilli, bounded := cpuCommitmentMilli(status.ResourceSpec, res); bounded {
			snapshot.Resources.CPU.AxnodedCommittedMilli += committedMilli
		} else {
			snapshot.Resources.CPU.AxnodedUnboundedCount++
		}
		if committedBytes, bounded := memoryCommitmentBytes(status.ResourceSpec, res); bounded {
			snapshot.Resources.Memory.AxnodedCommittedBytes += committedBytes
		} else {
			snapshot.Resources.Memory.AxnodedUnboundedCount++
		}
	}
	sort.Strings(snapshot.Components.Axnoded.RunningAllocationIDs)
	sort.Strings(snapshot.Components.Axnoded.ActiveAllocationIDs)

	if !s.resourcePoolDisabled(resources.CgroupResourceName) {
		cgroupPool, err := s.container.ResourcePoolStatus(resources.CgroupResourceName)
		if err != nil {
			snapshot.Sources["axnoded"] = errorSource(err)
			snapshot.Components.Axnoded.Status = StatusError
			snapshot.Components.Axnoded.Error = err.Error()
			return false
		}
		snapshot.Pools.Cgroup = poolInventoryFromStatus(cgroupPool)
	}

	if !s.resourcePoolDisabled(resources.InterfaceResourceName) {
		interfacePool, err := s.container.ResourcePoolStatus(resources.InterfaceResourceName)
		if err != nil {
			snapshot.Sources["axnoded"] = errorSource(err)
			snapshot.Components.Axnoded.Status = StatusError
			snapshot.Components.Axnoded.Error = err.Error()
			return false
		}
		snapshot.Pools.Interface = poolInventoryFromStatus(interfacePool)
	}
	snapshot.Pools.RuntimeSlots = s.runtimeSlotInventory(len(allContainers), snapshot.Pools)

	status, componentStatus, componentError := s.collectAxnodedActualUsage(now, runningContainers, allContainers, snapshot)
	snapshot.Sources["axnoded"] = status
	snapshot.Components.Axnoded.Status = componentStatus
	snapshot.Components.Axnoded.Error = componentError
	return true
}

func (s *AxnodedSource) runtimeSlotInventory(active int, pools PoolsInventory) PoolInventory {
	effectiveCapacity := s.runtimeSlotCapacity
	using := active
	warmIdle := -1
	configuredPools := []struct {
		name resources.ResourceName
		pool PoolInventory
	}{
		{name: resources.CgroupResourceName, pool: pools.Cgroup},
		{name: resources.InterfaceResourceName, pool: pools.Interface},
	}
	for _, configured := range configuredPools {
		name, pool := configured.name, configured.pool
		if s.resourcePoolDisabled(name) {
			continue
		}
		using = max(using, pool.Using)
		effectiveCapacity = min(effectiveCapacity, max(0, pool.Capacity-pool.Unavailable))
		if warmIdle < 0 {
			warmIdle = pool.Idle
		} else {
			warmIdle = min(warmIdle, pool.Idle)
		}
	}
	available := max(0, effectiveCapacity-using)
	if warmIdle < 0 {
		// No enabled resource pool needs materialization, so every available
		// runtime slot is immediately reusable.
		warmIdle = available
	}
	return PoolInventory{
		Using:       using,
		Idle:        min(max(0, warmIdle), available),
		Capacity:    s.runtimeSlotCapacity,
		Unavailable: s.runtimeSlotCapacity - effectiveCapacity,
	}
}

func poolInventoryFromStatus(status resources.PoolStatus) PoolInventory {
	return PoolInventory{
		Using:       status.Using,
		Idle:        status.Idle,
		Capacity:    status.Capacity,
		Unavailable: status.Unavailable,
	}
}

func (s *AxnodedSource) collectAxnodedActualUsage(now time.Time, runningContainers, allocationContainers []*container.Container, snapshot *NodeInventorySnapshot) (SourceStatus, string, string) {
	var retiringLeases []resources.RetiringMemoryLease
	if s.retiringMemoryLeases != nil {
		retiringLeases = s.retiringMemoryLeases()
	}
	retiringAllocations := make(map[string]struct{}, len(retiringLeases))
	for _, lease := range retiringLeases {
		if allocationID := strings.TrimSpace(lease.AllocationID); allocationID != "" {
			retiringAllocations[allocationID] = struct{}{}
		}
	}
	if len(allocationContainers) == 0 && len(retiringLeases) == 0 {
		recordAllocationMemoryMetrics(nil)
		return readySource(now), StatusReady, ""
	}
	if !s.memoryCgroupEnforced {
		// disabled_dev deliberately does not create an allocation-owned memcg.
		// A runtime may therefore report the delegated root (commonly "/") as
		// its cgroup path. Sampling that shared domain once per allocation would
		// fabricate attribution and double-count host usage. Keep the durable
		// reservation totals above, but publish no per-allocation usage or CPU
		// sample unless the production cgroup ownership contract is enforced.
		s.sampleMu.Lock()
		s.prevCPUSamples = make(map[string]cpuUsageSample)
		s.sampleMu.Unlock()
		recordAllocationMemoryMetrics(nil)
		return readySource(now), StatusReady, ""
	}
	if s.cgroupDriver == nil {
		err := fmt.Errorf("cgroup driver unavailable")
		return errorSource(err), StatusError, err.Error()
	}

	currentSamples := make(map[string]cpuUsageSample, len(runningContainers))
	var usedMilli int64
	status := readySource(now)
	componentStatus := StatusReady
	var errs []string
	warming := false
	successes := 0
	memoryByRuntime := newAllocationMemoryMetricSet()
	var memoryObservationRevision int64
	nextMemoryRevision := func() (int64, error) {
		if memoryObservationRevision > 0 {
			return memoryObservationRevision, nil
		}
		if s.memoryObservationRevision == nil {
			return 0, fmt.Errorf("durable observation revision provider is unavailable")
		}
		revision, err := s.memoryObservationRevision()
		if err != nil {
			return 0, fmt.Errorf("allocate durable observation revision: %w", err)
		}
		memoryObservationRevision = revision
		return revision, nil
	}
	memoryObservationIDs := make(map[string]struct{})
	appendMemoryObservation := func(observation *nodev1.AllocationMemoryObservation) {
		allocationID := strings.TrimSpace(observation.GetAllocationID())
		if _, duplicate := memoryObservationIDs[allocationID]; duplicate {
			errs = append(errs, fmt.Sprintf("%s memory: duplicate allocation ownership during inventory", allocationID))
			return
		}
		memoryObservationIDs[allocationID] = struct{}{}
		snapshot.AllocationMemoryObservations = append(snapshot.AllocationMemoryObservations, observation)
		snapshot.Resources.Memory.AxnodedUsedBytes = saturatingInt64Add(snapshot.Resources.Memory.AxnodedUsedBytes, observation.GetCurrentBytes())
		runtimeName := observation.GetRuntime()
		if memoryByRuntime[runtimeName] == nil {
			memoryByRuntime[runtimeName] = make(map[string]float64)
		}
		for kind, value := range map[string]float64{
			"current": float64(observation.GetCurrentBytes()), "peak": float64(observation.GetPeakBytes()),
			"swap_current": float64(observation.GetSwapCurrentBytes()),
			"anon":         float64(observation.GetAnonBytes()), "file": float64(observation.GetFileBytes()), "shmem": float64(observation.GetShmemBytes()),
			"kernel": float64(observation.GetKernelBytes()), "file_dirty": float64(observation.GetDirtyBytes()), "file_writeback": float64(observation.GetWritebackBytes()),
			"event_high": float64(observation.GetEventHigh()), "event_max": float64(observation.GetEventMax()),
			"event_oom": float64(observation.GetEventOom()), "event_oom_kill": float64(observation.GetEventOomKill()),
			"event_oom_group_kill": float64(observation.GetEventOomGroupKill()),
			"psi_some_total_usec":  float64(observation.GetPsiSomeTotalUsec()), "psi_full_total_usec": float64(observation.GetPsiFullTotalUsec()),
		} {
			memoryByRuntime[runtimeName][kind] += value
		}
		for kind, value := range map[string]float64{
			"psi_some_avg10": observation.GetPsiSomeAvg10(), "psi_full_avg10": observation.GetPsiFullAvg10(),
		} {
			if value > memoryByRuntime[runtimeName][kind] {
				memoryByRuntime[runtimeName][kind] = value
			}
		}
		if observation.GetPsiAvailable() {
			memoryByRuntime[runtimeName]["psi_available_allocations"]++
		}
	}

	for _, c := range runningContainers {
		if c == nil || c.Metadata == nil {
			continue
		}
		if _, retiring := retiringAllocations[strings.TrimSpace(c.Metadata.ID)]; retiring {
			errs = append(errs, fmt.Sprintf("%s memory: running container also has retiring cgroup ownership", c.Metadata.ID))
			continue
		}
		cgroupPath, err := s.container.RuntimeCgroupPath(c.Metadata.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Metadata.ID, err))
			continue
		}
		stats, err := s.loadCgroupStats(cgroupPath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Metadata.ID, err))
			continue
		}
		statusValue := c.Status.Get()
		memoryLimit := statusValue.ResourceSpec.GetLimits().GetMemoryBytes()
		if s.memoryBudgetEnabled && allocationAttempt(c) > 0 {
			revision, revisionErr := nextMemoryRevision()
			if revisionErr != nil {
				errs = append(errs, fmt.Sprintf("%s memory: %v", c.Metadata.ID, revisionErr))
				continue
			}
			observation, observationErr := allocationMemoryObservation(c, cgroupPath, memoryLimit, revision, now)
			if observationErr != nil {
				errs = append(errs, fmt.Sprintf("%s memory: %v", c.Metadata.ID, observationErr))
				continue
			}
			if s.memoryPIDRolesVerifier != nil {
				observation.PidRolesVerified = s.memoryPIDRolesVerifier(
					c.Metadata.ID,
					c.Metadata.GetRuntimeHandler(),
					cgroupPath,
					statusValue.Pid,
				) == nil
			}
			appendMemoryObservation(observation)
		} else {
			// Node-local and development containers are included in aggregate
			// host usage, but only control-plane allocations carry the durable
			// attempt identity required by the public observation contract.
			memoryUsage := int64(stats.MemoryUsage)
			if stats.MemoryUsage > math.MaxInt64 {
				memoryUsage = math.MaxInt64
			}
			snapshot.Resources.Memory.AxnodedUsedBytes = saturatingInt64Add(snapshot.Resources.Memory.AxnodedUsedBytes, memoryUsage)
		}
		successes++
		currentSamples[c.Metadata.ID] = cpuUsageSample{UsageNs: stats.CPUUsageTotal, CollectedAt: now}

		s.sampleMu.Lock()
		prev, ok := s.prevCPUSamples[c.Metadata.ID]
		s.sampleMu.Unlock()
		if !ok || !prev.CollectedAt.Before(now) {
			warming = true
			continue
		}
		current := cpuUsageSample{UsageNs: stats.CPUUsageTotal, CollectedAt: now}
		deltaMilli, ok := cpuUsedMilli(prev, current)
		if !ok {
			warming = true
			continue
		}
		usedMilli += deltaMilli
	}

	// A memcg remains allocation-owned after workload exit and until ordered
	// Delete turns its durable lease into retiring cleanup debt. Continue
	// sampling those exited domains so the terminal OOM event delta, final peak,
	// and post-exit page-cache charge are not lost merely because CPU sampling
	// no longer applies.
	runningIDs := make(map[string]struct{}, len(runningContainers))
	for _, c := range runningContainers {
		if c != nil && c.Metadata != nil {
			runningIDs[c.Metadata.ID] = struct{}{}
		}
	}
	for _, c := range allocationContainers {
		if c == nil || c.Metadata == nil || c.Status == nil {
			continue
		}
		if _, running := runningIDs[c.Metadata.ID]; running {
			continue
		}
		if _, retiring := retiringAllocations[strings.TrimSpace(c.Metadata.ID)]; retiring {
			// Recycle durably changes ownership before the container record is
			// removed. The retiring ledger is authoritative in that window.
			continue
		}
		statusValue := c.Status.Get()
		memoryLimit := statusValue.ResourceSpec.GetLimits().GetMemoryBytes()
		if !s.memoryBudgetEnabled || allocationAttempt(c) <= 0 {
			continue
		}
		cgroupPath, err := s.container.RuntimeCgroupPath(c.Metadata.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s exited memory: %v", c.Metadata.ID, err))
			continue
		}
		revision, revisionErr := nextMemoryRevision()
		if revisionErr != nil {
			errs = append(errs, fmt.Sprintf("%s exited memory: %v", c.Metadata.ID, revisionErr))
			continue
		}
		observation, observationErr := allocationMemoryObservation(c, cgroupPath, memoryLimit, revision, now)
		if observationErr != nil {
			errs = append(errs, fmt.Sprintf("%s exited memory: %v", c.Metadata.ID, observationErr))
			continue
		}
		// No live runtime PID role is expected after terminal exit. Control and
		// cgroup identity remain fully verified; periodic enforcement audits own
		// PID-role fail-stop decisions while the allocation is running.
		observation.PidRolesVerified = false
		appendMemoryObservation(observation)
		successes++
	}

	for _, lease := range retiringLeases {
		revision, revisionErr := nextMemoryRevision()
		if revisionErr != nil {
			errs = append(errs, fmt.Sprintf("%s retiring memory: %v", lease.AllocationID, revisionErr))
			continue
		}
		observation, observationErr := retiringMemoryObservation(s.cgroupDriver, lease, revision, now)
		if observationErr != nil {
			if errors.Is(observationErr, os.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s retiring memory: %v", lease.AllocationID, observationErr))
			continue
		}
		appendMemoryObservation(observation)
		successes++
	}

	s.sampleMu.Lock()
	s.prevCPUSamples = currentSamples
	s.sampleMu.Unlock()
	snapshot.Resources.CPU.AxnodedUsedMilli = usedMilli
	if warming {
		status = SourceStatus{Status: StatusWarming}
		componentStatus = StatusWarming
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		errMsg := strings.Join(errs, "; ")
		if successes == 0 {
			status = errorSource(fmt.Errorf("%s", errMsg))
			componentStatus = StatusError
			return status, componentStatus, errMsg
		}
		status = degradedSource(errMsg, now)
		if componentStatus != StatusWarming {
			componentStatus = StatusDegraded
		}
		return status, componentStatus, errMsg
	}
	recordAllocationMemoryMetrics(memoryByRuntime)
	return status, componentStatus, ""
}

func newAllocationMemoryMetricSet() map[string]map[string]float64 {
	values := make(map[string]map[string]float64, len(allocationMemoryMetricRuntimes))
	for _, runtimeName := range allocationMemoryMetricRuntimes {
		values[runtimeName] = make(map[string]float64, len(allocationMemoryMetricKinds))
		for _, kind := range allocationMemoryMetricKinds {
			values[runtimeName][kind] = 0
		}
	}
	return values
}

func recordAllocationMemoryMetrics(values map[string]map[string]float64) {
	if values == nil {
		values = newAllocationMemoryMetricSet()
	}
	for _, runtimeName := range allocationMemoryMetricRuntimes {
		for _, kind := range allocationMemoryMetricKinds {
			metrics.RecordCgroupMemory(runtimeName, kind, values[runtimeName][kind])
		}
	}
}

func saturatingInt64Add(current, delta int64) int64 {
	if delta <= 0 {
		return current
	}
	if current > math.MaxInt64-delta {
		return math.MaxInt64
	}
	return current + delta
}

func retiringMemoryObservation(driver os2.CgroupDriver, lease resources.RetiringMemoryLease, revision int64, now time.Time) (*nodev1.AllocationMemoryObservation, error) {
	if driver == nil || lease.CgroupID == "" || lease.AllocationID == "" || lease.AllocationAttempt <= 0 || lease.MemoryRequest < 0 || lease.MemoryLimit < 0 || revision <= 0 {
		return nil, fmt.Errorf("retiring allocation memory metadata is incomplete")
	}
	if lease.MemoryLimit > 0 && lease.MemoryRequest > lease.MemoryLimit {
		return nil, fmt.Errorf("retiring allocation memory request exceeds its limit")
	}
	workloadPath := os2.WorkloadGroup(lease.CgroupID, driver.Mode())
	domain, err := hostlinux.InspectCgroupMemoryParent(lease.CgroupID)
	if err != nil {
		return nil, err
	}
	bounded := lease.MemoryLimit > 0
	if bounded && (domain.LimitBytes != lease.MemoryLimit || domain.SwapMaxBytes != 0 || !domain.OOMGroup) {
		return nil, fmt.Errorf("retiring memory enforcement differs from allocation limit")
	}
	if lease.BootID != "" || lease.MountIdentity != "" || lease.ParentInode != 0 || lease.LeafInode != 0 {
		if lease.BootID == "" || lease.MountIdentity == "" || lease.ParentInode == 0 || lease.LeafInode == 0 {
			return nil, fmt.Errorf("retiring allocation has partial cgroup identity")
		}
		if domain.BootID != lease.BootID || domain.MountIdentity != lease.MountIdentity || domain.ParentInode != lease.ParentInode {
			return nil, fmt.Errorf("retiring allocation cgroup identity changed")
		}
	}
	leafControlsVerified := false
	fullDomain, fullErr := hostlinux.InspectCgroupMemoryDomain(lease.CgroupID, workloadPath)
	if fullErr == nil {
		if lease.LeafInode != 0 && fullDomain.LeafInode != lease.LeafInode {
			return nil, fmt.Errorf("retiring workload cgroup identity changed")
		}
		domain = fullDomain
		leafControlsVerified = bounded
	} else if !errors.Is(fullErr, os.ErrNotExist) {
		return nil, fullErr
	}
	usage, err := hostlinux.ReadCgroupMemoryObservation(lease.CgroupID)
	if err != nil {
		return nil, err
	}
	return memoryObservationFromKernel(
		lease.AllocationID, lease.AllocationAttempt, lease.MemoryRequest, lease.MemoryLimit, lease.RuntimeName,
		nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING, revision, now, domain, usage, bounded, leafControlsVerified,
	), nil
}

func allocationMemoryObservation(c *container.Container, workloadPath string, limitBytes, revision int64, now time.Time) (*nodev1.AllocationMemoryObservation, error) {
	if c == nil || c.Metadata == nil || c.Status == nil || limitBytes < 0 || revision <= 0 {
		return nil, fmt.Errorf("allocation memory metadata is incomplete")
	}
	parentPath := ""
	if c.Spec != nil {
		parentPath = strings.TrimSpace(c.Spec.Annotations[resources.ResourceAnnotationKeyPrefix+string(resources.CgroupResourceName)])
	}
	if parentPath == "" {
		parentPath = path.Dir(strings.TrimSpace(workloadPath))
	}
	domain, err := hostlinux.InspectCgroupMemoryDomain(parentPath, workloadPath)
	if err != nil {
		return nil, err
	}
	bounded := limitBytes > 0
	if bounded && (domain.LimitBytes != limitBytes || domain.SwapMaxBytes != 0 || !domain.OOMGroup) {
		return nil, fmt.Errorf("memory enforcement differs from allocation limit")
	}
	usage, err := hostlinux.ReadCgroupMemoryObservation(parentPath)
	if err != nil {
		return nil, err
	}
	attempt := allocationAttempt(c)
	if attempt <= 0 {
		return nil, fmt.Errorf("allocation attempt is unavailable")
	}
	requestBytes := c.Status.Get().ResourceSpec.GetRequests().GetMemoryBytes()
	if requestBytes < 0 || (bounded && requestBytes > limitBytes) {
		return nil, fmt.Errorf("allocation memory request is inconsistent with its limit")
	}
	return memoryObservationFromKernel(
		c.Metadata.ID, attempt, requestBytes, limitBytes, c.Metadata.GetRuntimeHandler(), nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_ASSIGNED, revision, now, domain, usage, bounded, bounded,
	), nil
}

func allocationAttempt(c *container.Container) int64 {
	if c == nil || c.Metadata == nil {
		return 0
	}
	attempt, err := strconv.ParseInt(strings.TrimSpace(c.Metadata.Labels[workloadidentity.LabelKeyAllocationAttempt]), 10, 64)
	if err != nil || attempt <= 0 {
		return 0
	}
	return attempt
}

func memoryObservationFromKernel(
	allocationID string,
	attempt, requestBytes, limitBytes int64,
	runtimeName string,
	cleanupState nodev1.AllocationMemoryCleanupState,
	revision int64,
	now time.Time,
	domain *hostlinux.CgroupMemoryDomain,
	usage *hostlinux.CgroupMemoryObservation,
	parentControlsVerified, leafControlsVerified bool,
) *nodev1.AllocationMemoryObservation {
	return &nodev1.AllocationMemoryObservation{
		AllocationID: allocationID, Attempt: attempt, Revision: revision, ObservedAt: timestamppb.New(now),
		RequestBytes: requestBytes, LimitBytes: limitBytes, CurrentBytes: usage.CurrentBytes, PeakBytes: usage.PeakBytes, PeakAvailable: usage.PeakAvailable,
		SwapCurrentBytes: usage.SwapCurrent, AnonBytes: usage.Stat["anon"], FileBytes: usage.Stat["file"],
		ShmemBytes: usage.Stat["shmem"], KernelBytes: usage.Stat["kernel"], DirtyBytes: usage.Stat["file_dirty"],
		WritebackBytes: usage.Stat["file_writeback"], EventHigh: usage.Events["high"], EventMax: usage.Events["max"],
		EventOom: usage.Events["oom"], EventOomKill: usage.Events["oom_kill"], EventOomGroupKill: usage.Events["oom_group_kill"],
		PsiSomeAvg10: usage.PSISomeAvg10, PsiFullAvg10: usage.PSIFullAvg10,
		PsiSomeTotalUsec: usage.PSISomeTotal, PsiFullTotalUsec: usage.PSIFullTotal,
		PsiAvailable:   usage.PSIAvailable,
		CgroupIdentity: fmt.Sprintf("boot=%s:%s:%d:%d", domain.BootID, domain.MountIdentity, domain.ParentInode, domain.LeafInode),
		Runtime:        runtimeName, ParentControlsVerified: parentControlsVerified, LeafControlsVerified: leafControlsVerified,
		CleanupState: cleanupState,
	}
}

func (s *AxnodedSource) loadCgroupStats(cgroupPath string) (*os2.CgroupStats, error) {
	cgroup, err := s.cgroupDriver.Load(cgroupPath)
	if err != nil {
		return nil, err
	}
	return cgroup.Stats()
}
