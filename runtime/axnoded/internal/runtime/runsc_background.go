package runtime

import (
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (r *RunscServiceHandler) BackgroundProcesses(allocationID string) (contract.BackgroundProcesses, error) {
	if !validPersistentContainerID(allocationID) || allocationID == ".." {
		return nil, errord.ErrInvalidArgument
	}
	path := runtimeoci.SandboxdBundleSocketPath(filepath.Join(r.common.ContainerRoot(), allocationID))
	return runtimesandboxd.NewClient(path), nil
}
