package imageprocess

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func ResolveMounts(targetSpec *specs.Spec, requests []*runtime.ImageProcessMount) ([]*runtime.Mount, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	if targetSpec == nil {
		return nil, fmt.Errorf("target sandbox OCI spec unavailable: %w", errord.ErrFailedPrecondition)
	}
	out := make([]*runtime.Mount, 0, len(requests))
	for _, request := range requests {
		mount, err := resolveMount(targetSpec.Mounts, request)
		if err != nil {
			return nil, err
		}
		out = append(out, mount)
	}
	return out, nil
}

func EnsureMountTargets(rootfsPath string, mounts []*runtime.Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	if rootfsPath == "" {
		return fmt.Errorf("image process rootfs path is required to prepare mount targets: %w", errord.ErrFailedPrecondition)
	}
	cleanRoot := filepath.Clean(rootfsPath)
	for _, mount := range mounts {
		if mount == nil || mount.GetType() != "bind" {
			continue
		}
		target, err := cleanAbsolutePath("target_path", mount.GetTarget())
		if err != nil {
			return err
		}
		if target == "/" {
			return fmt.Errorf("image process mount target / is not allowed: %w", errord.ErrInvalidArgument)
		}
		sourceInfo, err := os.Stat(mount.GetSource())
		if err != nil {
			return fmt.Errorf("stat image process mount source %q: %w", mount.GetSource(), err)
		}
		if err := ensureMountTarget(cleanRoot, target, sourceInfo); err != nil {
			return fmt.Errorf("prepare image process mount target %q: %w", target, err)
		}
	}
	return nil
}

func ensureMountTarget(rootfsPath string, target string, sourceInfo os.FileInfo) error {
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
			if err := os.Mkdir(current, 0755); err != nil {
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
	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("%w: %w", errord.ErrFailedPrecondition, err)
	}
	return file.Close()
}

func resolveMount(sourceMounts []specs.Mount, request *runtime.ImageProcessMount) (*runtime.Mount, error) {
	if request == nil {
		return nil, fmt.Errorf("image process mount is required: %w", errord.ErrInvalidArgument)
	}
	sandboxPath, err := cleanAbsolutePath("sandbox_path", request.GetSandboxPath())
	if err != nil {
		return nil, err
	}
	targetPath, err := cleanAbsolutePath("target_path", request.GetTargetPath())
	if err != nil {
		return nil, err
	}

	var match *specs.Mount
	var matchDest string
	for idx := range sourceMounts {
		mount := &sourceMounts[idx]
		if mount.Source == "" || !filepath.IsAbs(mount.Source) {
			continue
		}
		if mount.Type != "" && mount.Type != "bind" {
			continue
		}
		dest := path.Clean(mount.Destination)
		if !pathMatchesMount(sandboxPath, dest) {
			continue
		}
		if len(dest) > len(matchDest) {
			match = mount
			matchDest = dest
		}
	}
	if match == nil {
		return nil, fmt.Errorf("sandbox path %q is not under a host-backed sandbox mount: %w", sandboxPath, errord.ErrFailedPrecondition)
	}

	hostPath, err := hostPathForSandboxMount(match.Source, matchDest, sandboxPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(hostPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("sandbox path %q is not backed by an existing host path: %w", sandboxPath, errord.ErrFailedPrecondition)
		}
		return nil, fmt.Errorf("stat host-backed sandbox path %q: %w", sandboxPath, err)
	}

	return &runtime.Mount{
		Type:    "bind",
		Source:  hostPath,
		Target:  targetPath,
		Options: MountOptions(request.GetReadonly(), request.GetOptions()),
	}, nil
}

func cleanAbsolutePath(field, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%s is required: %w", field, errord.ErrInvalidArgument)
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("%s %q must be absolute: %w", field, raw, errord.ErrInvalidArgument)
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("%s %q must not contain path traversal: %w", field, raw, errord.ErrInvalidArgument)
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%s %q must be absolute: %w", field, raw, errord.ErrInvalidArgument)
	}
	return cleaned, nil
}

func pathMatchesMount(sandboxPath, mountDest string) bool {
	if mountDest == "" || mountDest == "." || !strings.HasPrefix(mountDest, "/") {
		return false
	}
	if sandboxPath == mountDest {
		return true
	}
	return strings.HasPrefix(sandboxPath, strings.TrimRight(mountDest, "/")+"/")
}

func hostPathForSandboxMount(source, mountDest, sandboxPath string) (string, error) {
	cleanSource := filepath.Clean(source)
	rel := strings.TrimPrefix(sandboxPath, mountDest)
	rel = strings.TrimPrefix(rel, "/")
	hostPath := filepath.Join(cleanSource, filepath.FromSlash(rel))
	if relToSource, err := filepath.Rel(cleanSource, hostPath); err != nil || relToSource == ".." || strings.HasPrefix(relToSource, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("sandbox path %q escapes host-backed mount: %w", sandboxPath, errord.ErrInvalidArgument)
	}
	return hostPath, nil
}

func MountOptions(readonly bool, requested []string) []string {
	options := make([]string, 0, len(requested)+2)
	seen := map[string]struct{}{}
	add := func(option string) {
		option = strings.TrimSpace(option)
		if option == "" {
			return
		}
		if _, ok := seen[option]; ok {
			return
		}
		seen[option] = struct{}{}
		options = append(options, option)
	}
	for _, option := range requested {
		trimmed := strings.TrimSpace(option)
		if trimmed == "ro" || trimmed == "rw" {
			continue
		}
		add(trimmed)
	}
	add("rbind")
	if readonly {
		add("ro")
	} else {
		add("rw")
	}
	return options
}
