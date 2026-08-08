//go:build linux

package nodeinventory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func storageMountFacts(path string) (string, string) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", ""
	}
	clean := filepath.Clean(path)
	bestLen := -1
	var fsType, identity string
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		pre, post := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(pre) < 5 || len(post) < 2 {
			continue
		}
		mountpoint := unescapeMountInfo(pre[4])
		if clean != mountpoint && !strings.HasPrefix(clean, strings.TrimSuffix(mountpoint, "/")+"/") {
			continue
		}
		if len(mountpoint) > bestLen {
			bestLen = len(mountpoint)
			fsType = post[0]
			identity = fmt.Sprintf("%s:%s:%s", pre[0], post[1], mountpoint)
		}
	}
	return fsType, identity
}

func unescapeMountInfo(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}
