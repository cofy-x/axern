//go:build linux

package hostlinux

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

func InspectEnforcedNodeMemoryBudget(physicalCapacityBytes, sourceAllocatableBytes, systemReserveBytes, conformanceMemoryMaxBytes int64, sandboxRootName string) (NodeMemoryBudgetSample, error) {
	if physicalCapacityBytes <= 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source physical memory capacity must be positive")
	}
	if sourceAllocatableBytes <= 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source memory allocatable must be positive")
	}
	if sourceAllocatableBytes > physicalCapacityBytes {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source memory allocatable %d exceeds physical capacity %d", sourceAllocatableBytes, physicalCapacityBytes)
	}
	if systemReserveBytes < 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("memory system reserve cannot be negative")
	}
	if conformanceMemoryMaxBytes <= 0 || conformanceMemoryMaxBytes > systemReserveBytes {
		return NodeMemoryBudgetSample{}, fmt.Errorf("runtime conformance memory maximum %d must fit within system reserve %d", conformanceMemoryMaxBytes, systemReserveBytes)
	}
	mountpoint := "/sys/fs/cgroup"
	if _, err := os.Stat(filepath.Join(mountpoint, "cgroup.controllers")); err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("unified cgroup v2 root is unavailable: %w", err)
	}
	sandboxRootName = strings.TrimSpace(sandboxRootName)
	if sandboxRootName == "" {
		return NodeMemoryBudgetSample{}, fmt.Errorf("resolved sandbox cgroup root is required")
	}
	sandboxDir := resourceDirForCgroupPath(sandboxRootName)
	delegationDir := filepath.Dir(sandboxDir)
	if delegationDir == sandboxDir || !pathWithinDirectory(mountpoint, delegationDir) {
		return NodeMemoryBudgetSample{}, fmt.Errorf("sandbox cgroup %s is outside the delegated cgroup v2 mount", sandboxRootName)
	}
	rootLimit, err := readCgroupInt64(filepath.Join(delegationDir, "memory.max"))
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	raw := sourceAllocatableBytes
	reportedRootLimit, finite := publicDelegatedRootLimit(rootLimit)
	if finite && rootLimit < raw {
		raw = rootLimit
	}
	if raw <= systemReserveBytes {
		return NodeMemoryBudgetSample{}, fmt.Errorf("memory system reserve %d leaves no sandbox capacity under raw allocatable %d", systemReserveBytes, raw)
	}
	internalDir := filepath.Join(delegationDir, "internal")
	conformanceDir := filepath.Join(delegationDir, "conformance")
	for label, dir := range map[string]string{"internal": internalDir, "conformance": conformanceDir, "sandbox": sandboxDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return NodeMemoryBudgetSample{}, fmt.Errorf("%s cgroup domain is unavailable at %s", label, dir)
		}
	}
	internalPath := "/" + strings.TrimPrefix(strings.TrimPrefix(internalDir, mountpoint), "/")
	if err := VerifyPIDInCgroup(internalPath, os.Getpid()); err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("axnoded is outside the internal memory domain: %w", err)
	}
	for _, file := range []string{filepath.Join(delegationDir, "cgroup.subtree_control"), filepath.Join(conformanceDir, "cgroup.subtree_control"), filepath.Join(sandboxDir, "cgroup.subtree_control")} {
		data, err := os.ReadFile(file)
		if err != nil || !containsCgroupController(string(data), "memory") {
			return NodeMemoryBudgetSample{}, fmt.Errorf("memory controller is not delegated through %s", file)
		}
	}
	internalCurrent, err := readCgroupInt64(filepath.Join(internalDir, "memory.current"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read internal cgroup memory: %w", err)
	}
	conformanceCurrent, err := readCgroupInt64(filepath.Join(conformanceDir, "memory.current"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read runtime conformance cgroup memory: %w", err)
	}
	conformanceLimit, err := readCgroupInt64(filepath.Join(conformanceDir, "memory.max"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read runtime conformance cgroup memory.max: %w", err)
	}
	if conformanceLimit != conformanceMemoryMaxBytes {
		return NodeMemoryBudgetSample{}, fmt.Errorf("runtime conformance cgroup memory.max is %d, want %d", conformanceLimit, conformanceMemoryMaxBytes)
	}
	conformanceSwap, err := readCgroupInt64(filepath.Join(conformanceDir, "memory.swap.max"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read runtime conformance cgroup memory.swap.max: %w", err)
	}
	if conformanceSwap != 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("runtime conformance cgroup memory.swap.max is %d, want 0", conformanceSwap)
	}
	conformanceOOMGroup, err := readCgroupInt64(filepath.Join(conformanceDir, "memory.oom.group"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read runtime conformance cgroup memory.oom.group: %w", err)
	}
	if conformanceOOMGroup != 1 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("runtime conformance cgroup memory.oom.group is %d, want 1", conformanceOOMGroup)
	}
	sandboxCurrent, err := readCgroupInt64(filepath.Join(sandboxDir, "memory.current"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read sandbox cgroup memory: %w", err)
	}
	_, mounted, mountIdentity, err := mountedFilesystemFacts(mountpoint)
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	if !mounted || mountIdentity == "" {
		return NodeMemoryBudgetSample{}, fmt.Errorf("cgroup mount identity is unavailable")
	}
	rootInode, err := cgroupInode(delegationDir)
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	sandboxInode, err := cgroupInode(sandboxDir)
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	conformanceInode, err := cgroupInode(conformanceDir)
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	bootID, err := CurrentBootID()
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	return NodeMemoryBudgetSample{
		PhysicalCapacityBytes:   physicalCapacityBytes,
		SourceAllocatableBytes:  sourceAllocatableBytes,
		DelegatedRootLimitBytes: reportedRootLimit, DelegatedRootLimitFinite: finite,
		SystemReserveBytes:      systemReserveBytes,
		EffectiveAllocatable:    raw - systemReserveBytes,
		InternalCurrentBytes:    internalCurrent,
		ConformanceCurrentBytes: conformanceCurrent,
		ConformanceLimitBytes:   conformanceLimit,
		SandboxCurrentBytes:     sandboxCurrent,
		CapacityIdentity:        fmt.Sprintf("cgroup-v2:boot=%s:%s:rootino=%x:sandboxino=%x:conformanceino=%x", bootID, mountIdentity, rootInode, sandboxInode, conformanceInode),
		SystemReserveExhausted:  systemReserveBytes > 0 && saturatingBudgetAdd(internalCurrent, conformanceCurrent) > systemReserveBytes,
	}, nil
}

func saturatingBudgetAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

// InspectDevelopmentNodeMemoryBudget publishes the resource-source capacity
// needed by local placement without claiming a delegated cgroup boundary. It
// is intentionally boot-scoped: node-local commitments from a previous boot
// must not be admitted against a new capacity generation.
func InspectDevelopmentNodeMemoryBudget(physicalCapacityBytes, sourceAllocatableBytes int64) (NodeMemoryBudgetSample, error) {
	if physicalCapacityBytes <= 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source physical memory capacity must be positive")
	}
	if sourceAllocatableBytes <= 0 {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source memory allocatable must be positive")
	}
	if sourceAllocatableBytes > physicalCapacityBytes {
		return NodeMemoryBudgetSample{}, fmt.Errorf("node resource source memory allocatable %d exceeds physical capacity %d", sourceAllocatableBytes, physicalCapacityBytes)
	}
	bootID, err := CurrentBootID()
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	return NodeMemoryBudgetSample{
		PhysicalCapacityBytes:  physicalCapacityBytes,
		SourceAllocatableBytes: sourceAllocatableBytes,
		EffectiveAllocatable:   sourceAllocatableBytes,
		CapacityIdentity:       fmt.Sprintf("disabled-dev:boot=%s:source=node-resources", bootID),
		SystemReserveExhausted: false,
	}, nil
}

func pathWithinDirectory(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func containsCgroupController(raw, want string) bool {
	for _, controller := range strings.Fields(raw) {
		if controller == want {
			return true
		}
	}
	return false
}
