package sandboxd

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	guestCgroupPath              = "/sys/fs/cgroup"
	guestTaskLimitStartupTimeout = 30 * time.Second
	guestTaskLimitPollInterval   = 25 * time.Millisecond
)

func validateGuestTaskLimit(limit int64) error {
	if limit != 0 && (limit < 64 || limit > 65536) {
		return fmt.Errorf("guest-task-limit must be zero or between 64 and 65536")
	}
	return nil
}

func waitForGuestTaskLimit(ctx context.Context, limit int64) error {
	if err := validateGuestTaskLimit(limit); err != nil {
		return err
	}
	if limit == 0 {
		return nil
	}
	if os.Getpid() != 1 {
		return fmt.Errorf("guest task limit requires sandboxd to run as PID 1")
	}
	ctx, cancel := context.WithTimeout(ctx, guestTaskLimitStartupTimeout)
	defer cancel()
	return waitForGuestTaskControls(ctx, limit, os.ReadFile)
}

func waitForGuestTaskControls(ctx context.Context, limit int64, readFile func(string) ([]byte, error)) error {
	if err := verifyGuestTaskBoundary(readFile); err != nil {
		return err
	}
	ticker := time.NewTicker(guestTaskLimitPollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for guest task controls: %w", err)
		}
		ready, err := guestTaskControlsReady(limit, readFile)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for guest task controls: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func verifyGuestTaskBoundary(readFile func(string) ([]byte, error)) error {
	cgroup, err := readFile("/proc/self/cgroup")
	if err != nil {
		return fmt.Errorf("read guest cgroup membership: %w", err)
	}
	if strings.TrimSpace(string(cgroup)) != "0::/" {
		return fmt.Errorf("guest task limit requires a private cgroup namespace rooted at the process cgroup")
	}
	mountinfo, err := readFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("read guest cgroup mount: %w", err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(mountinfo)), "\n") {
		before, after, ok := strings.Cut(line, " - ")
		fields := strings.Fields(before)
		if !ok || len(fields) < 6 {
			return fmt.Errorf("invalid guest mountinfo")
		}
		if strings.HasPrefix(fields[4], guestCgroupPath+"/") {
			return fmt.Errorf("guest cgroup controls are obscured by a nested mount")
		}
		if fields[4] != guestCgroupPath {
			continue
		}
		filesystem := strings.Fields(after)
		if found || len(filesystem) < 3 || filesystem[0] != "cgroup2" {
			return fmt.Errorf("guest task limit requires one cgroup2 mount at %s", guestCgroupPath)
		}
		if !slices.Contains(strings.Split(filesystem[2], ","), "nsdelegate") {
			return fmt.Errorf("guest cgroup mount requires nsdelegate to protect task controls")
		}
		readOnly := false
		for _, option := range strings.Split(fields[5], ",") {
			if option == "rw" {
				return fmt.Errorf("guest cgroup mount must be read-only")
			}
			if option == "ro" {
				readOnly = true
			}
		}
		if !readOnly {
			return fmt.Errorf("guest cgroup mount must be read-only")
		}
		found = true
	}
	if !found {
		return fmt.Errorf("guest task limit requires a read-only cgroup2 mount at %s", guestCgroupPath)
	}
	return nil
}

func guestTaskControlsReady(limit int64, readFile func(string) ([]byte, error)) (bool, error) {
	ready := true
	for _, control := range []struct {
		name string
		want int64
	}{
		{"cgroup.max.depth", 0},
		{"cgroup.max.descendants", 0},
		{"pids.max", limit},
	} {
		data, err := readFile(guestCgroupPath + "/" + control.name)
		if os.IsNotExist(err) {
			ready = false
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read guest %s: %w", control.name, err)
		}
		value := strings.TrimSpace(string(data))
		if value == "max" {
			ready = false
			continue
		}
		n, err := parseGuestTaskCount(value)
		if err != nil || n != control.want {
			return false, fmt.Errorf("guest %s does not match required value %d", control.name, control.want)
		}
	}
	if !ready {
		return false, nil
	}
	data, err := readFile(guestCgroupPath + "/pids.current")
	if err != nil {
		return false, fmt.Errorf("read guest pids.current: %w", err)
	}
	current, err := parseGuestTaskCount(strings.TrimSpace(string(data)))
	if err != nil || current < 1 || current > limit {
		return false, fmt.Errorf("guest pids.current must be between 1 and the task limit %d", limit)
	}
	data, err = readFile(guestCgroupPath + "/cgroup.stat")
	if err != nil {
		return false, fmt.Errorf("read guest cgroup.stat: %w", err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return false, fmt.Errorf("invalid guest cgroup.stat")
		}
		if fields[0] == "nr_descendants" {
			count, err := parseGuestTaskCount(fields[1])
			if found || err != nil || count != 0 {
				return false, fmt.Errorf("guest cgroup must not contain descendant cgroups")
			}
			found = true
		}
	}
	if !found {
		return false, fmt.Errorf("guest cgroup.stat is missing nr_descendants")
	}
	return true, nil
}

func parseGuestTaskCount(value string) (int64, error) {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, fmt.Errorf("invalid task count")
	}
	return strconv.ParseInt(value, 10, 64)
}
