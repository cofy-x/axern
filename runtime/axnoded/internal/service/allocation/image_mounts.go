package allocation

import (
	"fmt"
	"path"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/sirupsen/logrus"
)

var protectedImageMountTargets = map[string]struct{}{
	"/":      {},
	"/bin":   {},
	"/dev":   {},
	"/etc":   {},
	"/lib":   {},
	"/lib64": {},
	"/mnt":   {},
	"/proc":  {},
	"/run":   {},
	"/sbin":  {},
	"/sys":   {},
	"/usr":   {},
}

func (h *Controller) resolveImageMounts(request *runtime.StartRequest) ([]*runtime.Mount, func(), error) {
	if request == nil || len(request.GetImageMounts()) == 0 {
		return nil, func() {}, nil
	}
	if h == nil || h.environmentCache == nil {
		return nil, nil, fmt.Errorf("image mount runtime manager is unavailable: %w", errord.ErrFailedPrecondition)
	}
	if err := validateImageMountTargets(request); err != nil {
		return nil, nil, err
	}

	mounts := make([]*runtime.Mount, 0, len(request.GetImageMounts()))
	roots := make([]*environmentcache.RootFS, 0, len(request.GetImageMounts()))
	cleanup := func() {
		releaseImageMountRoots(roots)
	}

	for _, imageMount := range request.GetImageMounts() {
		if imageMount == nil {
			continue
		}
		cfg := environmentcache.RootfsConfig{
			SrcType:          runtime.RootfsSrcType_IMAGE,
			ImageUrl:         strings.TrimSpace(imageMount.GetImage()),
			DockerConfigJSON: strings.TrimSpace(request.GetRegistryCredential().GetDockerConfigJson()),
		}
		resolved, err := h.environmentCache.ResolveRootfsConfig(cfg)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("resolve image mount %q: %w", cfg.ImageUrl, err)
		}
		rootfs, err := h.environmentCache.GetRootfs(resolved)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("mount image %q: %w", cfg.ImageUrl, err)
		}
		if err := rootfs.IncActiveRef(); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("retain image mount %q: %w", cfg.ImageUrl, err)
		}
		roots = append(roots, rootfs)
		mounts = append(mounts, &runtime.Mount{
			Type:    "bind",
			Source:  rootfs.Path(),
			Target:  path.Clean(strings.TrimSpace(imageMount.GetTarget())),
			Options: []string{"rbind", "ro"},
		})
	}

	if err := h.rememberImageMountRoots(request.GetAllocationID(), roots, request.GetImageMounts()); err != nil {
		releaseImageMountRoots(roots)
		return nil, nil, fmt.Errorf("persist image mount ownership: %w", err)
	}
	return mounts, func() {
		h.forgetImageMountRoots(request.GetAllocationID())
	}, nil
}

func validateImageMountTargets(request *runtime.StartRequest) error {
	seen := map[string]struct{}{}
	for _, imageMount := range request.GetImageMounts() {
		if imageMount == nil {
			return fmt.Errorf("image mount is required: %w", errord.ErrInvalidArgument)
		}
		image := strings.TrimSpace(imageMount.GetImage())
		if image == "" {
			return fmt.Errorf("image mount image is required: %w", errord.ErrInvalidArgument)
		}
		rawTarget := strings.TrimSpace(imageMount.GetTarget())
		target := path.Clean(rawTarget)
		if target == "." || !strings.HasPrefix(target, "/") || pathHasParentReference(rawTarget) {
			return fmt.Errorf("image mount target %q must be an absolute container path below /: %w", rawTarget, errord.ErrInvalidArgument)
		}
		if _, protected := protectedImageMountTargets[target]; protected {
			return fmt.Errorf("image mount target %q is protected: %w", target, errord.ErrInvalidArgument)
		}
		for existing := range seen {
			if containerPathsOverlap(existing, target) {
				return fmt.Errorf("image mount target %q overlaps image mount target %q: %w", target, existing, errord.ErrInvalidArgument)
			}
		}
		if err := validateImageMountTargetDoesNotOverlapMounts(target, request.GetEnvironment().GetMounts()); err != nil {
			return err
		}
		if err := validateImageMountTargetDoesNotOverlapMounts(target, request.GetMounts()); err != nil {
			return err
		}
		for _, secretFile := range request.GetSecretFiles() {
			if secretFile == nil {
				continue
			}
			secretTarget := path.Clean(strings.TrimSpace(secretFile.GetPath()))
			if containerPathsOverlap(target, secretTarget) {
				return fmt.Errorf("image mount target %q overlaps secret file target %q: %w", target, secretTarget, errord.ErrInvalidArgument)
			}
		}
		seen[target] = struct{}{}
	}
	return nil
}

func validateImageMountTargetDoesNotOverlapMounts(target string, mounts []*runtime.Mount) error {
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		other := path.Clean(strings.TrimSpace(mount.GetTarget()))
		if other == "." || other == "" {
			continue
		}
		if containerPathsOverlap(target, other) {
			return fmt.Errorf("image mount target %q overlaps sandbox mount target %q: %w", target, other, errord.ErrInvalidArgument)
		}
	}
	return nil
}

func pathHasParentReference(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func containerPathsOverlap(a, b string) bool {
	a = path.Clean(a)
	b = path.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func releaseImageMountRoots(roots []*environmentcache.RootFS) {
	for _, rootfs := range roots {
		if rootfs == nil {
			continue
		}
		if released := rootfs.ReleaseActiveRef(); released {
			logrus.WithField("rootfs_type", rootfs.RootfsTypeLabel()).Debug("released image mount rootfs")
		}
	}
}
