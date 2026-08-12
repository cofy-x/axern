//go:build linux

package hostlinux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func InspectEnforcedNodeMemoryBudget(physicalCapacityBytes, sourceAllocatableBytes, systemReserveBytes int64, sandboxRootName string) (NodeMemoryBudgetSample, error) {
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
	for label, dir := range map[string]string{"internal": internalDir, "sandbox": sandboxDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return NodeMemoryBudgetSample{}, fmt.Errorf("%s cgroup domain is unavailable at %s", label, dir)
		}
	}
	internalPath := "/" + strings.TrimPrefix(strings.TrimPrefix(internalDir, mountpoint), "/")
	if err := VerifyPIDInCgroup(internalPath, os.Getpid()); err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("axnoded is outside the internal memory domain: %w", err)
	}
	for _, file := range []string{filepath.Join(delegationDir, "cgroup.subtree_control"), filepath.Join(sandboxDir, "cgroup.subtree_control")} {
		data, err := os.ReadFile(file)
		if err != nil || !containsCgroupController(string(data), "memory") {
			return NodeMemoryBudgetSample{}, fmt.Errorf("memory controller is not delegated through %s", file)
		}
	}
	internalCurrent, err := readCgroupInt64(filepath.Join(internalDir, "memory.current"))
	if err != nil {
		return NodeMemoryBudgetSample{}, fmt.Errorf("read internal cgroup memory: %w", err)
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
	bootID, err := CurrentBootID()
	if err != nil {
		return NodeMemoryBudgetSample{}, err
	}
	return NodeMemoryBudgetSample{
		PhysicalCapacityBytes:   physicalCapacityBytes,
		SourceAllocatableBytes:  sourceAllocatableBytes,
		DelegatedRootLimitBytes: reportedRootLimit, DelegatedRootLimitFinite: finite,
		SystemReserveBytes:     systemReserveBytes,
		EffectiveAllocatable:   raw - systemReserveBytes,
		InternalCurrentBytes:   internalCurrent,
		SandboxCurrentBytes:    sandboxCurrent,
		CapacityIdentity:       fmt.Sprintf("cgroup-v2:boot=%s:%s:rootino=%x:sandboxino=%x", bootID, mountIdentity, rootInode, sandboxInode),
		SystemReserveExhausted: systemReserveBytes > 0 && internalCurrent > systemReserveBytes,
	}, nil
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
