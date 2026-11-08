package startplan

import (
	"fmt"
	"path"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func validateWorkspace(request *runtime.StartRequest) error {
	if request.GetWorkspace() == nil {
		return nil
	}
	if _, err := workspace.FromBinding(request.GetWorkspace()); err != nil {
		return fmt.Errorf("%v: %w", err, errord.ErrInvalidArgument)
	}
	if request.GetRootfsSnapshot() != nil {
		return fmt.Errorf("Workspace and rootfs snapshot cannot be combined: %w", errord.ErrInvalidArgument)
	}
	if request.GetResources().GetLimits().GetMemoryBytes() <= 0 {
		return fmt.Errorf("Workspace requires a finite memory limit and complete runtime ownership: %w", errord.ErrInvalidArgument)
	}
	conflict := func(target string) bool {
		target = path.Clean(target)
		return target == "/" || target == "/workspace" || strings.HasPrefix(target, "/workspace/") || strings.HasPrefix("/workspace/", target+"/")
	}
	for _, mounts := range [][]*runtime.Mount{request.GetMounts(), request.GetEnvironment().GetMounts()} {
		for _, mount := range mounts {
			if conflict(mount.GetTarget()) {
				return fmt.Errorf("mount overlaps managed /workspace: %w", errord.ErrInvalidArgument)
			}
		}
	}
	for _, mount := range request.GetImageMounts() {
		if conflict(mount.GetTarget()) {
			return fmt.Errorf("image mount overlaps managed /workspace: %w", errord.ErrInvalidArgument)
		}
	}
	for _, secret := range request.GetSecretFiles() {
		if conflict(secret.GetPath()) {
			return fmt.Errorf("Secret projection overlaps retained Workspace: %w", errord.ErrInvalidArgument)
		}
	}
	return nil
}
