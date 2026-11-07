//go:build linux

package hostlinux

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// KillRunscCgroup terminates the exact admitted workload leaf without thawing
// a paused guest. The directory FD pins the verified kernel object through the
// cgroup.kill write, so a replaced path cannot redirect the signal.
func KillRunscCgroup(path string, sentryPID int, runtimeBinary, bootID, mountIdentity string, inode uint64) error {
	dir, err := openRunscCgroup(path, sentryPID, bootID, mountIdentity, inode)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	if err := VerifyRunscCgroupProcesses(path, sentryPID, runtimeBinary); err != nil {
		return fmt.Errorf("verify sandbox fail-stop process attribution: %w", err)
	}
	fd, err := unix.Openat(dir, "cgroup.kill", unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open sandbox cgroup.kill: %w", err)
	}
	defer unix.Close(fd)
	n, err := unix.Write(fd, []byte("1"))
	if err != nil {
		return fmt.Errorf("kill sandbox cgroup: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("short sandbox cgroup.kill write")
	}
	return nil
}

func openRunscCgroup(path string, sentryPID int, bootID, mountIdentity string, inode uint64) (int, error) {
	if !filepath.IsAbs(path) || path == "/" || filepath.Clean(path) != path || strings.TrimSpace(path) != path || bootID == "" || mountIdentity == "" || inode == 0 || sentryPID <= 0 {
		return -1, fmt.Errorf("sandbox fail-stop requires complete cgroup and Sentry identity")
	}
	currentBoot, err := CurrentBootID()
	if err != nil {
		return -1, fmt.Errorf("read sandbox fail-stop boot identity: %w", err)
	}
	if currentBoot != bootID {
		return -1, fmt.Errorf("sandbox fail-stop boot identity changed")
	}
	_, mounted, currentMount, err := mountedFilesystemFacts("/sys/fs/cgroup")
	if err != nil {
		return -1, fmt.Errorf("read sandbox fail-stop cgroup mount identity: %w", err)
	}
	if !mounted || currentMount != mountIdentity {
		return -1, fmt.Errorf("sandbox fail-stop cgroup mount identity changed")
	}
	dir, err := unix.Open(resourceDirForCgroupPath(path), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open sandbox fail-stop cgroup: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			unix.Close(dir)
		}
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(dir, &stat); err != nil {
		return -1, err
	}
	if stat.Ino != inode {
		return -1, fmt.Errorf("sandbox fail-stop workload cgroup identity changed")
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(dir, &fs); err != nil {
		return -1, err
	}
	if fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return -1, fmt.Errorf("sandbox fail-stop requires the unified cgroup filesystem")
	}
	ok = true
	return dir, nil
}

// RunscCgroupStopped confirms kernel termination despite a stale OCI paused
// status. A reused Sentry PID is ambiguous and must never become exit evidence.
func RunscCgroupStopped(path string, sentryPID int, bootID, mountIdentity string, inode uint64) (bool, error) {
	if !filepath.IsAbs(path) || path == "/" || filepath.Clean(path) != path || strings.TrimSpace(path) != path || bootID == "" || mountIdentity == "" || inode == 0 || sentryPID <= 0 {
		return false, fmt.Errorf("sandbox termination verification requires complete identity")
	}
	boot, err := CurrentBootID()
	if err != nil {
		return false, err
	}
	if boot != bootID {
		return false, fmt.Errorf("admitted runtime belongs to a previous kernel boot; automatic recovery is unavailable")
	}
	dir, err := openRunscCgroup(path, sentryPID, bootID, mountIdentity, inode)
	if errors.Is(err, os.ErrNotExist) {
		return recordedSentryGone(sentryPID)
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(dir)
	return cgroupStoppedAt(dir, sentryPID)
}

// ConformanceCgroupStopped only observes terminal evidence for a legacy
// node-owned certification lease without a memory-domain identity. The caller
// must verify its exact durable typed ownership; this path never signals any
// process or derives authority to continue a sandbox. Checking the parent
// covers all descendants, including an old gofer left outside the workload.
func ConformanceCgroupStopped(path string, sentryPID int) (bool, error) {
	if !filepath.IsAbs(path) || path == "/" || filepath.Clean(path) != path || strings.TrimSpace(path) != path || sentryPID <= 0 {
		return false, fmt.Errorf("conformance termination verification requires exact cgroup and Sentry identity")
	}
	var rootFS unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &rootFS); err != nil {
		return false, err
	}
	if rootFS.Type != unix.CGROUP2_SUPER_MAGIC {
		return false, fmt.Errorf("conformance termination requires the unified cgroup filesystem")
	}
	dir, err := unix.Open(resourceDirForCgroupPath(path), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return recordedSentryGone(sentryPID)
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(dir)
	var fs unix.Statfs_t
	if err := unix.Fstatfs(dir, &fs); err != nil {
		return false, err
	}
	if fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return false, fmt.Errorf("conformance termination requires the unified cgroup filesystem")
	}
	return cgroupStoppedAt(dir, sentryPID)
}

func cgroupStoppedAt(dir, sentryPID int) (bool, error) {
	fd, err := unix.Openat(dir, "cgroup.events", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), "cgroup.events")
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return false, err
	}
	populated := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			if populated != "" {
				return false, fmt.Errorf("duplicate cgroup population evidence")
			}
			populated = fields[1]
		}
	}
	if populated == "1" {
		return false, nil
	}
	if populated != "0" {
		return false, fmt.Errorf("invalid cgroup population evidence")
	}
	return recordedSentryGone(sentryPID)
}

func recordedSentryGone(sentryPID int) (bool, error) {
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(sentryPID))); err == nil {
		return false, fmt.Errorf("empty or removed workload cgroup retains or reuses its recorded Sentry PID")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}
