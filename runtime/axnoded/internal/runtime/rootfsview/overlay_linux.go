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
	return resolveOverlayLowerDirsFromInfo(rootDir, mountInfo)
}

func resolveOverlayLowerDirsFromInfo(rootDir string, mountInfo mountInfoEntry) ([]string, error) {
	if err := validateOverlayPath("effective rootfs", rootDir); err != nil {
		return nil, err
	}
	if err := validateCanonicalAbsolutePath("rootfs mountpoint", mountInfo.mountpoint); err != nil {
		return nil, err
	}
	if err := validateCanonicalAbsolutePath("rootfs mount root", mountInfo.mountRoot); err != nil {
		return nil, err
	}
	if mountInfo.fsType != "overlay" {
		return []string{rootDir}, nil
	}

	lowerDirValue := mountOptionValue(mountInfo.superOptions, "lowerdir")
	if lowerDirValue == "" {
		return nil, fmt.Errorf("writable rootfs view source is overlay but lowerdir is missing: %s", rootDir)
	}
	if strings.Contains(lowerDirValue, `\`) {
		return nil, fmt.Errorf("overlay lowerdir requires unsupported mount-option escaping: %s", rootDir)
	}
	lowerDirs := strings.Split(lowerDirValue, ":")
	if upperDir := mountOptionValue(mountInfo.superOptions, "upperdir"); upperDir != "" {
		lowerDirs = append([]string{upperDir}, lowerDirs...)
	}
	for _, lowerDir := range lowerDirs {
		if err := validateOverlayPath("overlay source", lowerDir); err != nil {
			return nil, err
		}
	}
	rel, err := filepath.Rel(mountInfo.mountpoint, rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve rootfs relative path %s from %s: %w", rootDir, mountInfo.mountpoint, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("effective rootfs %s escapes mountpoint %s", rootDir, mountInfo.mountpoint)
	}
	mountRoot := strings.TrimPrefix(filepath.Clean(mountInfo.mountRoot), string(os.PathSeparator))
	if rel == "." && mountRoot == "" {
		return lowerDirs, nil
	}
	out := make([]string, 0, len(lowerDirs))
	for _, lowerDir := range lowerDirs {
		effective := filepath.Join(lowerDir, mountRoot, rel)
		if err := validateOverlayPath("effective overlay source", effective); err != nil {
			return nil, err
		}
		out = append(out, effective)
	}
	return out, nil
}

func validateOverlayPath(name, candidate string) error {
	if err := validateCanonicalAbsolutePath(name, candidate); err != nil {
		return err
	}
	if strings.ContainsAny(candidate, `,:\`) || strings.ContainsAny(candidate, "\x00\n\r\t") {
		return fmt.Errorf("%s contains an unsupported mount-option delimiter: %s", name, candidate)
	}
	return nil
}

func validateCanonicalAbsolutePath(name, candidate string) error {
	if candidate == "" || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return fmt.Errorf("%s must be a canonical absolute path: %s", name, candidate)
	}
	if strings.ContainsAny(candidate, "\x00\n\r\t") {
		return fmt.Errorf("%s contains a control character", name)
	}
	return nil
}

func mountOverlayView(rootfs overlayView) error {
	if len(rootfs.LowerDirs) == 0 {
		return fmt.Errorf("overlay lowerdir is required")
	}
	for _, candidate := range append(append([]string(nil), rootfs.LowerDirs...), rootfs.UpperDir, rootfs.WorkDir, rootfs.MergedDir) {
		if err := validateOverlayPath("overlay path", candidate); err != nil {
			return err
		}
	}
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
	mountID      int
	mountRoot    string
	mountpoint   string
	fsType       string
	source       string
	mountOptions string
	superOptions string
}

func InspectBacking(rootDir string) (RootfsBackingFacts, error) {
	info, err := mountInfoForPath(rootDir)
	if err != nil {
		return RootfsBackingFacts{}, err
	}
	lowerDirs, err := resolveOverlayLowerDirsFromInfo(rootDir, info)
	if err != nil {
		return RootfsBackingFacts{}, err
	}
	var statfs unix.Statfs_t
	if err := unix.Statfs(rootDir, &statfs); err != nil {
		return RootfsBackingFacts{}, fmt.Errorf("statfs rootfs backing %s: %w", rootDir, err)
	}
	lowerChain := make([]RootfsBackingLayerFacts, 0, len(lowerDirs))
	for _, lowerDir := range lowerDirs {
		layer, layerErr := inspectBackingLayer(lowerDir)
		if layerErr != nil {
			return RootfsBackingFacts{}, fmt.Errorf("inspect effective rootfs lower %s: %w", lowerDir, layerErr)
		}
		lowerChain = append(lowerChain, layer)
	}
	return RootfsBackingFacts{
		EffectiveRoot: filepath.Clean(rootDir), MountID: info.mountID, Mountpoint: info.mountpoint, MountRoot: info.mountRoot,
		FSType: info.fsType, Source: info.source,
		Readonly: mountOptionsContain(info.mountOptions, "ro") || statfs.Flags&unix.ST_RDONLY != 0, LowerDirs: lowerDirs,
		EffectiveLowerChain: lowerChain,
	}, nil
}

func inspectBackingLayer(path string) (RootfsBackingLayerFacts, error) {
	info, err := mountInfoForPath(path)
	if err != nil {
		return RootfsBackingLayerFacts{}, err
	}
	var statfs unix.Statfs_t
	if err := unix.Statfs(path, &statfs); err != nil {
		return RootfsBackingLayerFacts{}, fmt.Errorf("statfs backing layer %s: %w", path, err)
	}
	return RootfsBackingLayerFacts{
		Path: filepath.Clean(path), MountID: info.mountID, Mountpoint: info.mountpoint,
		MountRoot: info.mountRoot, FSType: info.fsType, Source: info.source,
		Readonly: mountOptionsContain(info.mountOptions, "ro") || statfs.Flags&unix.ST_RDONLY != 0,
	}, nil
}

func verifyMountedOverlay(path string) error {
	info, err := mountInfoForPath(path)
	if err != nil {
		return err
	}
	if info.mountpoint != filepath.Clean(path) || info.fsType != "overlay" {
		return fmt.Errorf("%s is not an active OverlayFS mount", path)
	}
	return nil
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
	if len(pre) < 6 || len(post) < 3 {
		return mountInfoEntry{}, false
	}
	mountID, err := strconv.Atoi(pre[0])
	if err != nil {
		return mountInfoEntry{}, false
	}
	return mountInfoEntry{
		mountID:      mountID,
		mountRoot:    unescapeMountInfoPath(pre[3]),
		mountpoint:   unescapeMountInfoPath(pre[4]),
		mountOptions: pre[5],
		fsType:       post[0],
		source:       unescapeMountInfoPath(post[1]),
		superOptions: post[2],
	}, true
}

func mountOptionsContain(options, want string) bool {
	for option := range strings.SplitSeq(options, ",") {
		if option == want {
			return true
		}
	}
	return false
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
