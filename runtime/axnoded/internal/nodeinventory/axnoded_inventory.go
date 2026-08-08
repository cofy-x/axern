package nodeinventory

import (
	"fmt"
	"sort"
	"strings"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
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

	status, componentStatus, componentError := s.collectAxnodedActualUsage(now, runningContainers, snapshot)
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

func (s *AxnodedSource) collectAxnodedActualUsage(now time.Time, runningContainers []*container.Container, snapshot *NodeInventorySnapshot) (SourceStatus, string, string) {
	if len(runningContainers) == 0 {
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

	for _, c := range runningContainers {
		if c == nil || c.Metadata == nil {
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
		if memory, err := hostlinux.ReadCgroupMemoryBreakdown(cgroupPath); err == nil {
			for _, kind := range []string{"anon", "file", "shmem", "file_dirty", "file_writeback", "event_oom", "event_oom_kill"} {
				metrics.RecordCgroupMemory(c.Metadata.GetRuntimeHandler(), c.Metadata.ID, kind, memory[kind])
			}
		}
		successes++
		snapshot.Resources.Memory.AxnodedUsedBytes += int64(stats.MemoryUsage)
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
	return status, componentStatus, ""
}

func (s *AxnodedSource) loadCgroupStats(cgroupPath string) (*os2.CgroupStats, error) {
	cgroup, err := s.cgroupDriver.Load(cgroupPath)
	if err != nil {
		return nil, err
	}
	return cgroup.Stats()
}
