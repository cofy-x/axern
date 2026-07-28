//go:build linux

package allocation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func prepareWorkspaceCOW(filestoreDir, containerID, lower string) (string, func(), error) {
	if filestoreDir == "" {
		return "", nil, fmt.Errorf("copy-on-write workspace requires runtime filestore_dir")
	}
	if containerID == "" || containerID == "." || containerID == ".." || filepath.Base(containerID) != containerID || strings.ContainsAny(containerID, `/\\`) {
		return "", nil, fmt.Errorf("copy-on-write workspace container id is invalid")
	}
	root := filepath.Join(filestoreDir, workspaceViewsDir, containerID)
	upper, work, merged := filepath.Join(root, "upper"), filepath.Join(root, "work"), filepath.Join(root, "merged")
	_ = cleanupWorkspaceCOW(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", nil, err
	}
	for _, dir := range []string{upper, work, merged} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = os.RemoveAll(root)
			return "", nil, err
		}
	}
	for _, mountPath := range []string{lower, upper, work} {
		if strings.ContainsAny(mountPath, ",:") {
			_ = os.RemoveAll(root)
			return "", nil, fmt.Errorf("copy-on-write workspace path contains an unsupported overlay separator")
		}
	}
	data := "lowerdir=" + lower + ",upperdir=" + upper + ",workdir=" + work
	if err := unix.Mount("overlay", merged, "overlay", 0, data); err != nil {
		_ = os.RemoveAll(root)
		return "", nil, fmt.Errorf("mount workspace overlay: %w", err)
	}
	// Overlayfs exposes the upperdir root metadata at the merged mount root.
	// Make that root writable with one metadata copy-up so arbitrary numeric
	// agent users can create files without recursively copying the workspace.
	if err := makeWorkspaceRootWritable(merged); err != nil {
		_ = unix.Unmount(merged, unix.MNT_DETACH)
		_ = os.RemoveAll(root)
		return "", nil, fmt.Errorf("make workspace overlay root writable: %w", err)
	}
	return merged, func() {
		if err := cleanupWorkspaceCOW(root); err != nil {
			logrus.WithError(err).WithField("workspace_view", root).Warn("failed to clean copy-on-write workspace")
		}
	}, nil
}

func makeWorkspaceRootWritable(merged string) error {
	return os.Chmod(merged, 0o777)
}

func restoreWorkspaceCOW(root, lower string) (string, error) {
	upper, work, merged := filepath.Join(root, "upper"), filepath.Join(root, "work"), filepath.Join(root, "merged")
	var stat unix.Statfs_t
	if err := unix.Statfs(merged, &stat); err == nil && stat.Type == unix.OVERLAYFS_SUPER_MAGIC {
		return merged, nil
	}
	for _, dir := range []string{upper, work, merged} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("workspace recovery directory %s is unavailable", dir)
		}
	}
	for _, mountPath := range []string{lower, upper, work} {
		if strings.ContainsAny(mountPath, ",:") {
			return "", fmt.Errorf("copy-on-write workspace path contains an unsupported overlay separator")
		}
	}
	data := "lowerdir=" + lower + ",upperdir=" + upper + ",workdir=" + work
	if err := unix.Mount("overlay", merged, "overlay", 0, data); err != nil {
		return "", fmt.Errorf("restore workspace overlay: %w", err)
	}
	if err := makeWorkspaceRootWritable(merged); err != nil {
		_ = unix.Unmount(merged, unix.MNT_DETACH)
		return "", fmt.Errorf("restore writable workspace root: %w", err)
	}
	return merged, nil
}

func cleanupWorkspaceCOW(root string) error {
	merged := filepath.Join(root, "merged")
	if err := unix.Unmount(merged, 0); err != nil && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOENT) {
		if !errors.Is(err, unix.EBUSY) {
			return err
		}
		if detachErr := unix.Unmount(merged, unix.MNT_DETACH); detachErr != nil && !errors.Is(detachErr, unix.EINVAL) && !errors.Is(detachErr, unix.ENOENT) {
			return detachErr
		}
	}
	return os.RemoveAll(root)
}
