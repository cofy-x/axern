package runtime

import (
	"context"
	"path/filepath"

	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (r *RunscServiceHandler) PreviewHTTP(ctx context.Context, allocationID string, request wire.HTTPPreviewRequest) (wire.HTTPPreviewResponse, error) {
	if !validPersistentContainerID(allocationID) {
		return wire.HTTPPreviewResponse{}, errord.ErrInvalidArgument
	}
	path := runtimeoci.SandboxdBundleSocketPath(filepath.Join(r.common.ContainerRoot(), allocationID))
	return runtimesandboxd.NewClient(path).PreviewHTTP(ctx, request)
}
