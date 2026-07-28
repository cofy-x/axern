//go:build linux

package rootfsview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func resolveOverlayLowerDirs(rootDir string) ([]string, error) {
	mountInfo, err := mountInfoForPath(rootDir)
	if err != nil {
		return nil, err
	}
	if mountInfo.fsType != "overlay" {
		return []string{rootDir}, nil
	}

	lowerDirValue := mountOptionValue(mountInfo.superOptions, "lowerdir")
	if lowerDirValue == "" {
		return nil, fmt.Errorf("writable rootfs view source is overlay but lowerdir is missing: %s", rootDir)
	}
	lowerDirs := strings.Split(lowerDirValue, ":")
	rel, err := filepath.Rel(mountInfo.mountpoint, rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve rootfs relative path %s from %s: %w", rootDir, mountInfo.mountpoint, err)
	}
	if rel == "." {
		return lowerDirs, nil
	}
	out := make([]string, 0, len(lowerDirs))
	for _, lowerDir := range lowerDirs {
		out = append(out, filepath.Join(lowerDir, rel))
	}
	return out, nil
}

func mountOverlayView(rootfs overlayView) error {
	mountData := strings.Join([]string{
		"lowerdir=" + strings.Join(rootfs.LowerDirs, ":"),
		"upperdir=" + rootfs.UpperDir,
		"workdir=" + rootfs.WorkDir,
	}, ",")
	if err := unix.Mount("overlay", rootfs.MergedDir, "overlay", 0, mountData); err != nil {
		return fmt.Errorf("mount writable rootfs view target=%s opts=%s: %w", rootfs.MergedDir, mountData, err)
	}
	return nil
}

func unmountOverlayView(rootfs overlayView) error {
	if rootfs.MergedDir == "" {
		return nil
	}
	if err := unix.Unmount(rootfs.MergedDir, 0); err != nil {
		if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("unmount writable rootfs view %s: %w", rootfs.MergedDir, err)
	}
	return nil
}

type mountInfoEntry struct {
	mountpoint   string
	fsType       string
	superOptions string
}

func mountInfoForPath(path string) (mountInfoEntry, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return mountInfoEntry{}, fmt.Errorf("read mountinfo: %w", err)
	}
	cleanPath := filepath.Clean(path)
	candidates := make([]mountInfoEntry, 0)
	for _, line := range strings.Split(string(data), "\n") {
		info, ok := parseMountInfoLine(line)
		if !ok {
			continue
		}
		if pathIsAtOrBelowMountpoint(cleanPath, info.mountpoint) {
			candidates = append(candidates, info)
		}
	}
	if len(candidates) == 0 {
		return mountInfoEntry{}, fmt.Errorf("mountinfo entry not found for %s", path)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].mountpoint) > len(candidates[j].mountpoint)
	})
	return candidates[0], nil
}

func pathIsAtOrBelowMountpoint(path, mountpoint string) bool {
	if path == mountpoint {
		return true
	}
	if mountpoint == string(os.PathSeparator) {
		return strings.HasPrefix(path, string(os.PathSeparator))
	}
	return strings.HasPrefix(path, mountpoint+string(os.PathSeparator))
}

func parseMountInfoLine(line string) (mountInfoEntry, bool) {
	if strings.TrimSpace(line) == "" {
		return mountInfoEntry{}, false
	}
	parts := strings.SplitN(line, " - ", 2)
	if len(parts) != 2 {
		return mountInfoEntry{}, false
	}
	pre := strings.Fields(parts[0])
	post := strings.Fields(parts[1])
	if len(pre) < 5 || len(post) < 3 {
		return mountInfoEntry{}, false
	}
	return mountInfoEntry{
		mountpoint:   unescapeMountInfoPath(pre[4]),
		fsType:       post[0],
		superOptions: post[2],
	}, true
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

func mountOptionValue(options, key string) string {
	prefix := key + "="
	for option := range strings.SplitSeq(options, ",") {
		if after, ok := strings.CutPrefix(option, prefix); ok {
			return after
		}
	}
	return ""
}
