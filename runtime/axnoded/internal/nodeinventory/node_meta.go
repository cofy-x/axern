package nodeinventory

import (
	"bufio"
	"maps"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func normalizeNodeState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "draining":
		return "draining"
	case "disabled":
		return "disabled"
	default:
		return "ready"
	}
}

func detectNodeCapacity() NodeResourceQuantity {
	return NodeResourceQuantity{
		CpuMilli:    int64(runtime.NumCPU() * 1000),
		MemoryBytes: detectMemoryTotalBytes(),
	}
}

func detectMemoryTotalBytes() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return value * 1024
	}
	return 0
}
