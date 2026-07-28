package cgroup

import (
	"errors"
	"fmt"
	stdos "os"
	"strconv"
	"strings"
)

func cpuCountFromCpusetFiles(paths ...string) (int, error) {
	var lastErr error
	for _, file := range paths {
		data, err := stdos.ReadFile(file)
		if err != nil {
			lastErr = err
			continue
		}
		count, err := parseCpusetCPUCount(strings.TrimSpace(string(data)))
		if err == nil {
			return count, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no cpuset file available")
	}
	return 0, lastErr
}

func parseCpusetCPUCount(cpus string) (int, error) {
	if cpus == "" {
		return 0, errors.New("cpuset is empty")
	}
	count := 0
	for _, item := range strings.Split(cpus, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "-") {
			parts := strings.Split(item, "-")
			if len(parts) != 2 {
				return 0, fmt.Errorf("invalid cpuset range: %q", item)
			}
			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return 0, err
			}
			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return 0, err
			}
			if end < start {
				return 0, fmt.Errorf("invalid cpuset range: %q", item)
			}
			count += end - start + 1
			continue
		}
		if _, err := strconv.Atoi(item); err != nil {
			return 0, err
		}
		count++
	}
	if count == 0 {
		return 0, errors.New("cpuset resolved to 0 cpus")
	}
	return count, nil
}
