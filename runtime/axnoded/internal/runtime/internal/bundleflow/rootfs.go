package bundleflow

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/jsonutil"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func ApplyRootfsView(bundlePath string, view rootfsview.View) error {
	if !view.Writable {
		return nil
	}
	if view.RootDir == "" {
		return fmt.Errorf("writable rootfs view root dir is required")
	}
	specPath := filepath.Join(bundlePath, config.ContainerSpecFile)
	ociSpec, err := runtimeoci.LoadSpec(specPath)
	if err != nil {
		return fmt.Errorf("load bundle spec for rootfs view: %w", err)
	}
	if ociSpec.Root == nil {
		ociSpec.Root = &spec.Root{}
	}
	ociSpec.Root.Path = view.RootDir
	ociSpec.Root.Readonly = false

	buf, err := jsonutil.UnescapedMarshal(ociSpec)
	if err != nil {
		return fmt.Errorf("marshal bundle spec for rootfs view: %w", err)
	}
	if err := os.WriteFile(specPath, buf, 0644); err != nil {
		return fmt.Errorf("write bundle spec for rootfs view: %w", err)
	}
	return nil
}

func PrepareBundleMountTargets(bundlePath string) error {
	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		return fmt.Errorf("load bundle spec for mount target preparation: %w", err)
	}
	if ociSpec.Root == nil || strings.TrimSpace(ociSpec.Root.Path) == "" {
		return nil
	}
	rootfsPath := ociSpec.Root.Path
	if !filepath.IsAbs(rootfsPath) {
		rootfsPath = filepath.Join(bundlePath, rootfsPath)
	}
	if err := prepareBundleMountTargets(rootfsPath, ociSpec.Mounts); err != nil {
		return fmt.Errorf("prepare bundle mount targets: %w", err)
	}
	return nil
}

func prepareBundleMountTargets(rootfsPath string, mounts []spec.Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	cleanRoot := filepath.Clean(rootfsPath)
	for _, mount := range mounts {
		if mount.Type != "bind" {
			continue
		}
		target := path.Clean(strings.TrimSpace(mount.Destination))
		if target == "." || target == "" || target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(mount.Destination) {
			return fmt.Errorf("bind mount target %q must be an absolute container path below /: %w", mount.Destination, errord.ErrInvalidArgument)
		}
		sourceInfo, err := os.Stat(mount.Source)
		if err != nil {
			return fmt.Errorf("stat bind mount source %q: %w", mount.Source, err)
		}
		if err := prepareBundleMountTarget(cleanRoot, target, sourceInfo); err != nil {
			return fmt.Errorf("prepare bind mount target %q: %w", target, err)
		}
	}
	return nil
}

func prepareBundleMountTarget(rootfsPath string, target string, sourceInfo os.FileInfo) error {
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	parentParts := parts
	if !sourceInfo.IsDir() {
		parentParts = parts[:len(parts)-1]
	}
	current := rootfsPath
	for _, part := range parentParts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return fmt.Errorf("%w: %w", errord.ErrFailedPrecondition, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("target path contains symlink: %w", errord.ErrFailedPrecondition)
		}
		if !info.IsDir() {
			return fmt.Errorf("target path parent exists but is not a directory: %w", errord.ErrFailedPrecondition)
		}
	}
	if sourceInfo.IsDir() {
		return nil
	}
	targetPath := filepath.Join(rootfsPath, filepath.FromSlash(strings.TrimPrefix(target, "/")))
	if info, err := os.Lstat(targetPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("target exists but is a symlink: %w", errord.ErrFailedPrecondition)
		}
		if info.IsDir() {
			return fmt.Errorf("target exists but is a directory: %w", errord.ErrFailedPrecondition)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("%w: %w", errord.ErrFailedPrecondition, err)
	}
	return file.Close()
}

func pathHasParentReference(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func RootfsViewSource(rootfs *apipb.Rootfs) rootfsview.Source {
	if rootfs == nil {
		return rootfsview.Source{}
	}
	return rootfsview.Source{
		RootDir:  rootfs.RootDir,
		Readonly: rootfs.Readonly,
	}
}
