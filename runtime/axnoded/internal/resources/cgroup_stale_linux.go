//go:build linux

package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

const cgroupFilesystemRoot = "/sys/fs/cgroup"

// staleDelegationCgroupProcesses inventories only the allocation parent and
// its OCI workload leaf. The durable lease and memory identity are checked by
// the caller before this path is used; unexpected children remain cleanup debt
// rather than being traversed or removed.
func staleDelegationCgroupProcesses(cgroupPath, rootName string) ([]int, error) {
	return staleDelegationCgroupProcessesAt(cgroupFilesystemRoot, cgroupPath, rootName)
}

func staleDelegationCgroupProcessesAt(filesystemRoot, cgroupPath, rootName string) ([]int, error) {
	parentDir, err := staleDelegationCgroupDir(filesystemRoot, cgroupPath, rootName)
	if err != nil {
		return nil, err
	}
	dirs, err := staleDelegationKnownDirs(parentDir)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]struct{})
	for index, dir := range dirs {
		payload, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
		if err != nil {
			// The runtime owns the workload leaf and may remove it after the
			// parent inventory was sampled. That is normal retirement progress;
			// only disappearance of the allocation parent proves the whole
			// stale domain is gone.
			if index > 0 && os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, field := range strings.Fields(string(payload)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 0 {
				return nil, fmt.Errorf("cgroup %s contains invalid pid %q", cgroupPath, field)
			}
			seen[pid] = struct{}{}
		}
	}
	result := make([]int, 0, len(seen))
	for pid := range seen {
		result = append(result, pid)
	}
	return result, nil
}

func removeStaleDelegationCgroup(cgroupPath, rootName string) error {
	return removeStaleDelegationCgroupAt(cgroupFilesystemRoot, cgroupPath, rootName)
}

func removeStaleDelegationCgroupAt(filesystemRoot, cgroupPath, rootName string) error {
	parentDir, err := staleDelegationCgroupDir(filesystemRoot, cgroupPath, rootName)
	if err != nil {
		return err
	}
	dirs, err := staleDelegationKnownDirs(parentDir)
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Remove(dirs[i]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale delegated cgroup %s: %w", dirs[i], err)
		}
	}
	return nil
}

func staleDelegationCgroupDir(filesystemRoot, cgroupPath, rootName string) (string, error) {
	clean := filepath.Clean(cgroupPath)
	if clean == "." || clean == string(filepath.Separator) || !filepath.IsAbs(clean) || clean != cgroupPath {
		return "", fmt.Errorf("stale cgroup path %q is not canonical and absolute", cgroupPath)
	}
	if rootName == "" || filepath.Base(filepath.Dir(clean)) != rootName {
		return "", fmt.Errorf("stale cgroup %q is outside an owned %s subtree", cgroupPath, rootName)
	}
	leaf := filepath.Base(clean)
	if len(leaf) != 12 {
		return "", fmt.Errorf("stale cgroup %q has an invalid allocation id", cgroupPath)
	}
	for _, char := range leaf {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return "", fmt.Errorf("stale cgroup %q has an invalid allocation id", cgroupPath)
		}
	}
	dir := filepath.Join(filesystemRoot, strings.TrimPrefix(clean, string(filepath.Separator)))
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("stale cgroup path %q is not a directory", cgroupPath)
	}
	return dir, nil
}

func staleDelegationKnownDirs(parentDir string) ([]string, error) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, err
	}
	dirs := []string{parentDir}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() != os2.CgroupWorkloadLeafName {
			return nil, fmt.Errorf("stale delegated cgroup %s contains unexpected child %q", parentDir, entry.Name())
		}
		child := filepath.Join(parentDir, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("stale delegated cgroup child %s is not a directory", child)
		}
		dirs = append(dirs, child)
	}
	return dirs, nil
}
