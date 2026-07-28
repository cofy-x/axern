package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func buildHardlinkTree(sourceRoot string, targetRoot string) error {
	info, err := os.Lstat(sourceRoot)
	if err != nil {
		return fmt.Errorf("failed to stat source lowerdir %s: %w", sourceRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source lowerdir %s is not a directory", sourceRoot)
	}
	if err := os.MkdirAll(targetRoot, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to create target lowerdir root %s: %w", targetRoot, err)
	}

	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return fmt.Errorf("failed to read source lowerdir %s: %w", sourceRoot, err)
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceRoot, entry.Name())
		targetPath := filepath.Join(targetRoot, entry.Name())

		entryInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to stat source entry %s: %w", sourcePath, err)
		}

		switch {
		case entryInfo.IsDir():
			if err := buildHardlinkTree(sourcePath, targetPath); err != nil {
				return err
			}
			if err := copyDirectoryMetadata(sourcePath, targetPath, entryInfo); err != nil {
				return err
			}
		case entryInfo.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(sourcePath)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", sourcePath, err)
			}
			if err := os.Symlink(linkTarget, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, linkTarget, err)
			}
		default:
			if err := os.Link(sourcePath, targetPath); err != nil {
				return fmt.Errorf("failed to hardlink %s -> %s: %w", sourcePath, targetPath, err)
			}
		}
	}

	return copyDirectoryMetadata(sourceRoot, targetRoot, info)
}

func copyDirectoryMetadata(sourcePath string, targetPath string, info os.FileInfo) error {
	if err := copyOwnership(targetPath, info); err != nil {
		return err
	}
	if err := os.Chmod(targetPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to copy mode for %s: %w", targetPath, err)
	}
	if err := copyTimes(targetPath, info); err != nil {
		return err
	}
	if err := copyXattrs(sourcePath, targetPath); err != nil {
		return err
	}
	return nil
}

func copyOwnership(targetPath string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Lchown(targetPath, int(stat.Uid), int(stat.Gid)); err != nil {
		if canIgnoreOwnershipError(err) {
			return nil
		}
		return fmt.Errorf("failed to copy ownership for %s: %w", targetPath, err)
	}
	return nil
}

func copyTimes(targetPath string, info os.FileInfo) error {
	mtime := info.ModTime()
	if err := os.Chtimes(targetPath, mtime, mtime); err != nil {
		return fmt.Errorf("failed to copy times for %s: %w", targetPath, err)
	}
	return nil
}

func copyXattrs(sourcePath string, targetPath string) error {
	names, err := listXattrs(sourcePath)
	if err != nil {
		if isIgnorableXattrErr(err) {
			return nil
		}
		return fmt.Errorf("failed to list xattrs for %s: %w", sourcePath, err)
	}
	for _, name := range names {
		value, err := getXattr(sourcePath, name)
		if err != nil {
			if isIgnorableXattrErr(err) {
				continue
			}
			return fmt.Errorf("failed to read xattr %s on %s: %w", name, sourcePath, err)
		}
		if err := unix.Setxattr(targetPath, name, value, 0); err != nil && !isIgnorableXattrErr(err) {
			return fmt.Errorf("failed to copy xattr %s to %s: %w", name, targetPath, err)
		}
	}
	return nil
}

func listXattrs(path string) ([]string, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	size, err = unix.Listxattr(path, buf)
	if err != nil {
		return nil, err
	}
	return splitNullTerminated(buf[:size]), nil
}

func getXattr(path string, name string) ([]byte, error) {
	size, err := unix.Getxattr(path, name, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, size)
	readSize, err := unix.Getxattr(path, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:readSize], nil
}

func splitNullTerminated(buf []byte) []string {
	if len(buf) == 0 {
		return nil
	}
	names := make([]string, 0, 4)
	start := 0
	for i, b := range buf {
		if b != 0 {
			continue
		}
		if i > start {
			names = append(names, string(buf[start:i]))
		}
		start = i + 1
	}
	if start < len(buf) {
		names = append(names, string(buf[start:]))
	}
	return names
}

func isIgnorableXattrErr(err error) bool {
	return errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.EPERM) ||
		errors.Is(err, unix.ENODATA)
}
