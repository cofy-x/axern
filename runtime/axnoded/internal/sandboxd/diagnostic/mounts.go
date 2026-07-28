package diagnostic

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type MountSnapshot struct {
	Mounts []Mount `json:"mounts"`
	Paths  []Path  `json:"paths,omitempty"`
}

type Mount struct {
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fsType"`
	Source     string `json:"source,omitempty"`
	Options    string `json:"options,omitempty"`
}

type Path struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Writable  bool   `json:"writable,omitempty"`
	Total     uint64 `json:"totalBytes,omitempty"`
	Available uint64 `json:"availableBytes,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Mounts(paths ...string) MountSnapshot {
	mounts := readMounts()
	if len(paths) == 0 {
		paths = []string{"/", "/mnt", "/proc"}
	}
	out := MountSnapshot{Mounts: mounts, Paths: make([]Path, 0, len(paths))}
	for _, path := range paths {
		out.Paths = append(out.Paths, inspectPath(path))
	}
	return out
}

func readMounts() []Mount {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	mounts := make([]Mount, 0)
	for _, line := range strings.Split(string(data), "\n") {
		mount, ok := parseMountInfoLine(line)
		if ok {
			mounts = append(mounts, mount)
		}
	}
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].Mountpoint < mounts[j].Mountpoint
	})
	return mounts
}

func parseMountInfoLine(line string) (Mount, bool) {
	if strings.TrimSpace(line) == "" {
		return Mount{}, false
	}
	parts := strings.SplitN(line, " - ", 2)
	if len(parts) != 2 {
		return Mount{}, false
	}
	pre := strings.Fields(parts[0])
	post := strings.Fields(parts[1])
	if len(pre) < 6 || len(post) < 3 {
		return Mount{}, false
	}
	return Mount{
		Mountpoint: unescapeMountInfoPath(pre[4]),
		Options:    pre[5],
		FSType:     post[0],
		Source:     post[1],
	}, true
}

func inspectPath(path string) Path {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." {
		path = "/"
	}
	out := Path{Path: path}
	if _, err := os.Stat(path); err != nil {
		out.Error = err.Error()
		return out
	}
	out.Exists = true
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err == nil {
		out.Total = stat.Blocks * uint64(stat.Bsize)
		out.Available = stat.Bavail * uint64(stat.Bsize)
	} else {
		out.Error = err.Error()
	}
	out.Writable = pathWritable(path)
	return out
}

func pathWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}

func unescapeMountInfoPath(path string) string {
	return mountInfoOctalEscapePattern.ReplaceAllStringFunc(path, func(match string) string {
		value, err := strconv.ParseInt(match[1:], 8, 32)
		if err != nil {
			return match
		}
		return string(rune(value))
	})
}
