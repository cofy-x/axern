package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

const (
	LocalParameterNamespace  = "namespace"
	LocalParameterServiceID  = "service_id"
	LocalParameterVolumeName = "volume_name"
)

type LocalProvider struct {
	root string
}

func NewLocalProvider(root string) *LocalProvider {
	return &LocalProvider{root: strings.TrimSpace(root)}
}

func (p *LocalProvider) Backend() storagev1.VolumeBackend {
	return storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL
}

func (p *LocalProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Backend: p.Backend(),
		AccessModes: []storagev1.VolumeAccessMode{
			storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		},
		ConsistencyProfiles: []storagev1.VolumeConsistencyProfile{
			storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		},
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}

func (p *LocalProvider) Publish(_ context.Context, _ string, volume *privatestoragev1.ResolvedNodeVolume) (*runtimevolumev1.PublishedVolume, error) {
	if volume == nil {
		return nil, fmt.Errorf("local volume is required")
	}
	if p.root == "" {
		return nil, fmt.Errorf("local volume root is required")
	}
	claimID := strings.TrimSpace(volume.GetClaimID())
	backendHandle := strings.TrimSpace(volume.GetBackendHandle())
	if claimID == "" || backendHandle == "" || claimID != backendHandle || !stablePathToken(claimID) {
		return nil, fmt.Errorf("local volume requires a matching stable claim id and backend handle")
	}
	target := cleanContainerTarget(volume.GetTarget())
	if target == "" {
		return nil, fmt.Errorf("local volume %q target must be an absolute container path below /", claimID)
	}
	for _, option := range volume.GetOptions() {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if !allowedMountOption(option) {
			return nil, fmt.Errorf("local volume %q option %q is not supported", claimID, option)
		}
	}
	source := filepath.Join(p.root, claimID)
	if err := os.MkdirAll(source, 0o750); err != nil {
		return nil, fmt.Errorf("create local volume %s: %w", source, err)
	}
	return &runtimevolumev1.PublishedVolume{
		ClaimID:   volume.GetClaimID(),
		BindingID: volume.GetBindingID(),
		Backend:   p.Backend(),
		HostPath:  source,
		Target:    target,
		Readonly:  volume.GetReadonly(),
		Options:   normalizeMountOptions(volume.GetOptions(), volume.GetReadonly()),
	}, nil
}

func (p *LocalProvider) Unpublish(context.Context, string, *runtimevolumev1.PublishedVolume) error {
	return nil
}

func (p *LocalProvider) Delete(_ context.Context, claimID, backendHandle string) error {
	claimID = strings.TrimSpace(claimID)
	backendHandle = strings.TrimSpace(backendHandle)
	if p.root == "" {
		return fmt.Errorf("local volume root is required")
	}
	if claimID == "" || backendHandle == "" || claimID != backendHandle || !stablePathToken(claimID) {
		return fmt.Errorf("local volume delete requires a matching stable claim id and backend handle")
	}
	root, err := filepath.Abs(p.root)
	if err != nil {
		return fmt.Errorf("resolve local volume root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect local volume root: %w", pathlessFilesystemError(err))
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local volume root must not be a symlink")
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("local volume root must be a directory")
	}
	source := filepath.Join(root, claimID)
	rel, err := filepath.Rel(root, source)
	if err != nil || rel != claimID || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("local volume path is outside local root")
	}
	info, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect local volume for claim %q: %w", claimID, pathlessFilesystemError(err))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local volume for claim %q must not be a symlink", claimID)
	}
	if !info.IsDir() {
		return fmt.Errorf("local volume for claim %q must be a directory", claimID)
	}
	if err := os.RemoveAll(source); err != nil {
		return fmt.Errorf("delete local volume for claim %q: %w", claimID, pathlessFilesystemError(err))
	}
	return nil
}

func pathlessFilesystemError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

func (p *LocalProvider) ValidatePublished(_ context.Context, _ string, volume *runtimevolumev1.PublishedVolume) error {
	if volume == nil {
		return InvalidPublishedVolumeError{Reason: "local published volume is required"}
	}
	hostPath := strings.TrimSpace(volume.GetHostPath())
	if hostPath == "" {
		return InvalidPublishedVolumeError{Reason: "local published volume host path is required"}
	}
	if !filepath.IsAbs(hostPath) {
		return InvalidPublishedVolumeError{Reason: fmt.Sprintf("local published volume host path %q must be absolute", hostPath)}
	}
	if strings.TrimSpace(p.root) == "" {
		return fmt.Errorf("local volume root is required")
	}
	root, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return fmt.Errorf("resolve local volume root: %w", err)
	}
	source, err := filepath.EvalSymlinks(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return InvalidPublishedVolumeError{Reason: fmt.Sprintf("local published volume host path %q not found", hostPath)}
		}
		return fmt.Errorf("resolve local volume host path: %w", err)
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return InvalidPublishedVolumeError{Reason: fmt.Sprintf("local published volume host path %q is outside local root", hostPath)}
	}
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return InvalidPublishedVolumeError{Reason: fmt.Sprintf("local published volume host path %q not found", hostPath)}
		}
		return fmt.Errorf("stat local published volume host path %q: %w", hostPath, err)
	}
	if !info.IsDir() {
		return InvalidPublishedVolumeError{Reason: fmt.Sprintf("local published volume host path %q is not a directory", hostPath)}
	}
	return nil
}

func cleanContainerTarget(value string) string {
	raw := strings.TrimSpace(value)
	target := path.Clean(raw)
	if target == "." || target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(raw) {
		return ""
	}
	return target
}

func allowedMountOption(option string) bool {
	switch option {
	case "ro", "rw", "rbind", "nosuid", "nodev", "noexec":
		return true
	default:
		return false
	}
}

func normalizeMountOptions(options []string, readonly bool) []string {
	seen := map[string]struct{}{"rbind": {}}
	out := []string{"rbind"}
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || option == "ro" || option == "rw" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		out = append(out, option)
	}
	sort.Strings(out[1:])
	if readonly {
		out = append(out, "ro")
	} else {
		out = append(out, "rw")
	}
	return out
}

func stablePathToken(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func pathHasParentReference(value string) bool {
	return slices.Contains(strings.Split(value, "/"), "..")
}
