package allocation

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
)

func (h *Controller) ActiveAllocationIDs() []string {
	if h == nil || h.containers() == nil {
		return nil
	}
	return activeAllocationIDs(h.containers().List())
}

func activeAllocationIDs(containers []*container.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(containers))
	for _, item := range containers {
		if item == nil || item.Metadata == nil {
			continue
		}
		ids = append(ids, item.Metadata.ID)
	}
	return servicevolumes.NormalizeAllocationIDs(ids)
}

func ValidateMountTargetsForRootfsReadonly(rootfsPath string, rootfsReadonly bool, mounts []*runtime.Mount) error {
	if !rootfsReadonly || strings.TrimSpace(rootfsPath) == "" || len(mounts) == 0 {
		return nil
	}
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		target := path.Clean(strings.TrimSpace(mount.GetTarget()))
		if target == "." || target == "" {
			continue
		}
		if target == "/" {
			continue
		}
		if !strings.HasPrefix(target, "/") {
			return fmt.Errorf("mount target %q must be an absolute container path", mount.GetTarget())
		}
		hostTarget := filepath.Join(rootfsPath, filepath.FromSlash(strings.TrimPrefix(target, "/")))
		if _, err := os.Stat(hostTarget); err == nil {
			continue
		} else if os.IsNotExist(err) {
			return fmt.Errorf("mount target %q does not exist in readonly rootfs", target)
		} else {
			return fmt.Errorf("stat mount target %q in readonly rootfs: %w", target, err)
		}
	}
	return nil
}
