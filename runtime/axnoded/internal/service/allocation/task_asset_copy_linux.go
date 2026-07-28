//go:build linux

package allocation

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// copyTaskAsset resolves every destination component relative to an already
// opened workspace fd. O_NOFOLLOW closes the check/use race with sandbox
// processes that may still be able to mutate the copy-on-write workspace.
func copyTaskAsset(source, workspaceRoot, destinationRelative string) error {
	rootFD, err := unix.Open(workspaceRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open workspace root: %w", err)
	}
	defer unix.Close(rootFD)

	sourceRoot := source
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		// The source is an immutable, allocation-owned payload mount. Overlayfs
		// may report synthetic link counts for lower files, so link count is not
		// a reliable file-type or confinement signal here. Path confinement and
		// O_NOFOLLOW protect the source/destination boundary.
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported task asset file type at %s", current)
		}
		rel, err := filepath.Rel(sourceRoot, current)
		if err != nil {
			return err
		}
		outputRel := destinationRelative
		if rel != "." {
			outputRel = filepath.Join(destinationRelative, rel)
		}
		components, err := safeRelativeComponents(outputRel)
		if err != nil {
			return err
		}
		if info.IsDir() {
			fd, err := openTaskAssetDir(rootFD, components, uint32(info.Mode().Perm()))
			if err == nil {
				err = unix.Close(fd)
			}
			return err
		}
		if len(components) == 0 {
			return fmt.Errorf("task asset file cannot replace workspace root")
		}
		parentFD, err := openTaskAssetDir(rootFD, components[:len(components)-1], 0o755)
		if err != nil {
			return err
		}
		in, err := os.Open(current)
		if err != nil {
			unix.Close(parentFD)
			return err
		}
		outFD, err := unix.Openat(parentFD, components[len(components)-1], unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(info.Mode().Perm()))
		unix.Close(parentFD)
		if err != nil {
			_ = in.Close()
			return err
		}
		out := os.NewFile(uintptr(outFD), components[len(components)-1])
		_, copyErr := io.Copy(out, in)
		inErr, outErr := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		return outErr
	})
}

func safeRelativeComponents(value string) ([]string, error) {
	clean := filepath.Clean(value)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("task asset target escapes workspace")
	}
	if clean == "." {
		return nil, nil
	}
	return strings.Split(clean, string(filepath.Separator)), nil
}

func openTaskAssetDir(rootFD int, components []string, mode uint32) (int, error) {
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, err
	}
	for _, component := range components {
		if err := unix.Mkdirat(current, component, mode); err != nil && err != unix.EEXIST {
			unix.Close(current)
			return -1, err
		}
		next, err := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(current)
		if err != nil {
			return -1, err
		}
		current = next
	}
	return current, nil
}
