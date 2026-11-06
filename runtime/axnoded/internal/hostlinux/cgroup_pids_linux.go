//go:build linux

package hostlinux

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConfigureCgroupPIDDomain bounds host tasks in the allocation subtree. The
// parent is the safety boundary; the workload leaf carries the same ceiling
// for the OCI runtime. This does not limit tasks inside the gVisor guest.
func ConfigureCgroupPIDDomain(parentPath, workloadPath string, limit int64) error {
	parentDir, workloadDir, err := cgroupPIDDomainDirs(parentPath, workloadPath, limit)
	if err != nil {
		return err
	}
	return configureCgroupPIDDomainDirs(parentDir, workloadDir, limit)
}

// VerifyCgroupPIDDomain reads both host task ceilings without repairing a
// missing, unlimited, or changed control.
func VerifyCgroupPIDDomain(parentPath, workloadPath string, limit int64) error {
	parentDir, workloadDir, err := cgroupPIDDomainDirs(parentPath, workloadPath, limit)
	if err != nil {
		return err
	}
	return verifyCgroupPIDDomainDirs(parentDir, workloadDir, limit)
}

func cgroupPIDDomainDirs(parentPath, workloadPath string, limit int64) (string, string, error) {
	if limit <= 0 {
		return "", "", fmt.Errorf("host task limit must be positive")
	}
	for _, path := range []string{parentPath, workloadPath} {
		if !filepath.IsAbs(path) || path == string(filepath.Separator) || filepath.Clean(path) != path || strings.TrimSpace(path) != path {
			return "", "", fmt.Errorf("host task enforcement requires clean absolute non-root cgroup paths")
		}
	}
	if filepath.Dir(workloadPath) != parentPath {
		return "", "", fmt.Errorf("workload cgroup %s is not the direct child of allocation cgroup %s", workloadPath, parentPath)
	}
	return resourceDirForCgroupPath(parentPath), resourceDirForCgroupPath(workloadPath), nil
}

func configureCgroupPIDDomainDirs(parentDir, workloadDir string, limit int64) error {
	if err := checkCgroupPIDDomainControls(parentDir, workloadDir, limit); err != nil {
		return err
	}
	want := strconv.FormatInt(limit, 10)
	for _, dir := range []string{parentDir, workloadDir} {
		if err := writeAndVerifyCgroupValue(filepath.Join(dir, "pids.max"), want); err != nil {
			return fmt.Errorf("configure host task ceiling: %w", err)
		}
	}
	return verifyCgroupPIDDomainDirs(parentDir, workloadDir, limit)
}

func verifyCgroupPIDDomainDirs(parentDir, workloadDir string, limit int64) error {
	if err := checkCgroupPIDDomainControls(parentDir, workloadDir, limit); err != nil {
		return err
	}
	want := strconv.FormatInt(limit, 10)
	for _, dir := range []string{parentDir, workloadDir} {
		path := filepath.Join(dir, "pids.max")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read host task ceiling %s: %w", path, err)
		}
		if got := strings.TrimSpace(string(data)); got != want {
			return fmt.Errorf("host task ceiling readback mismatch for %s: got=%q want=%q", path, got, want)
		}
	}
	return nil
}

func checkCgroupPIDDomainControls(parentDir, workloadDir string, limit int64) error {
	if limit <= 0 {
		return fmt.Errorf("host task limit must be positive")
	}
	// Check both domains before writing either ceiling. Missing delegation
	// must not create a file or quietly leave an allocation unbounded.
	for _, dir := range []string{parentDir, workloadDir} {
		data, err := os.ReadFile(filepath.Join(dir, "cgroup.type"))
		if err != nil {
			return fmt.Errorf("read host task cgroup-v2 domain %s: %w", dir, err)
		}
		if strings.TrimSpace(string(data)) != "domain" {
			return fmt.Errorf("host task cgroup %s is not a cgroup-v2 domain", dir)
		}
		if _, err := os.ReadFile(filepath.Join(dir, "pids.max")); err != nil {
			return fmt.Errorf("read host task control %s: %w", dir, err)
		}
	}
	return nil
}
