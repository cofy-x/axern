package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Chain roots are ordered from oldest to newest. They retain each layer's
// visibility markers, while their implicit directory stat metadata has already
// been resolved against the preceding chain. Only those stat attributes inherit.
type overlayAttributeReader func(string, string) ([]byte, bool, error)

func inheritImplicitDirectoryMetadata(targetRoot string, previousRoots, implicit []string, attributes overlayAttributeReader) error {
	if implicit == nil {
		return fmt.Errorf("layer has no complete implicit-directory facts")
	}
	paths := append([]string(nil), implicit...)
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] > paths[j]
	})
	for _, name := range paths {
		if name == "." || !filepath.IsLocal(name) || filepath.Clean(name) != name {
			return fmt.Errorf("invalid implicit directory fact %q", name)
		}
		barrier, err := implicitDirectoryAncestorBarrier(targetRoot, name, attributes)
		if err != nil {
			return err
		}
		if barrier == opaqueBarrier {
			continue
		}
		if barrier == unqualifiedBarrier {
			return fmt.Errorf("implicit metadata depends on an unqualified current-layer opaque namespace")
		}
		info, err := effectiveDirectoryMetadata(previousRoots, name, attributes)
		if err != nil {
			return fmt.Errorf("resolve implicit directory %s: %w", name, err)
		}
		if info == nil {
			continue
		}
		if err := copyDirectoryStatMetadata(filepath.Join(targetRoot, name), info); err != nil {
			return err
		}
	}
	return nil
}

func implicitDirectoryAncestorBarrier(root, name string, attributes overlayAttributeReader) (opaqueState, error) {
	barrier, err := directoryOpaqueBarrier(root, attributes)
	if err != nil {
		return noOpaqueBarrier, err
	}
	parts := strings.Split(name, string(filepath.Separator))
	for i := range parts {
		entry := filepath.Join(append([]string{root}, parts[:i+1]...)...)
		info, err := os.Lstat(entry)
		if err != nil {
			return noOpaqueBarrier, err
		}
		if !info.IsDir() {
			return noOpaqueBarrier, fmt.Errorf("implicit directory fact crosses a non-directory at %s", entry)
		}
		if i < len(parts)-1 {
			state, err := directoryOpaqueBarrier(entry, attributes)
			if err != nil {
				return noOpaqueBarrier, err
			}
			if state > barrier {
				barrier = state
			}
		}
	}
	return barrier, nil
}

func effectiveDirectoryMetadata(previousRoots []string, name string, attributes overlayAttributeReader) (os.FileInfo, error) {
	parts := strings.Split(name, string(filepath.Separator))
	for i := len(previousRoots) - 1; i >= 0; i-- {
		root := previousRoots[i]
		barrier, err := directoryOpaqueBarrier(root, attributes)
		if err != nil {
			return nil, err
		}
		for part := range parts {
			entry := filepath.Join(append([]string{root}, parts[:part+1]...)...)
			info, err := os.Lstat(entry)
			if os.IsNotExist(err) {
				if barrier == opaqueBarrier {
					return nil, nil
				}
				if barrier == unqualifiedBarrier {
					return nil, fmt.Errorf("implicit metadata depends on an unqualified overlay xattr namespace")
				}
				break
			}
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				if part == len(parts)-1 {
					return info, nil
				}
				state, err := directoryOpaqueBarrier(entry, attributes)
				if err != nil {
					return nil, err
				}
				if state > barrier {
					barrier = state
				}
				continue
			}
			if isDeviceWhiteout(info) {
				return nil, nil
			}
			if info.Mode().IsRegular() {
				whiteout, qualified, err := attributes(entry, "whiteout")
				if err != nil {
					return nil, err
				}
				if whiteout != nil {
					if qualified {
						return nil, nil
					}
					return nil, fmt.Errorf("implicit metadata depends on an unqualified whiteout namespace")
				}
			}
			return nil, fmt.Errorf("implicit directory crosses a visible non-directory at %s", entry)
		}
	}
	return nil, nil
}

type opaqueState uint8

const (
	noOpaqueBarrier opaqueState = iota
	unqualifiedBarrier
	opaqueBarrier
)

func directoryOpaqueBarrier(path string, attributes overlayAttributeReader) (opaqueState, error) {
	value, qualified, err := attributes(path, "opaque")
	if err != nil || value == nil {
		return noOpaqueBarrier, err
	}
	switch string(value) {
	case "x":
		return noOpaqueBarrier, nil
	case "y":
		if qualified {
			return opaqueBarrier, nil
		}
		return unqualifiedBarrier, nil
	default:
		return noOpaqueBarrier, fmt.Errorf("unsupported overlay opaque metadata at %s", path)
	}
}

// The current lowerdir mount profile does not select userxattr. A user-only
// fallback marker is observable, but cannot prove that the kernel hides lower
// entries. Such a marker is never permission to publish fresh, wider metadata.
func overlayAttribute(path, name string) ([]byte, bool, error) {
	for _, prefix := range []string{"trusted.overlay.", "user.overlay."} {
		value, err := getXattr(path, prefix+name)
		if errors.Is(err, unix.ENODATA) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return value, prefix == "trusted.overlay.", nil
	}
	return nil, true, nil
}

func isDeviceWhiteout(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode()&os.ModeCharDevice != 0 && stat.Rdev == 0
}
