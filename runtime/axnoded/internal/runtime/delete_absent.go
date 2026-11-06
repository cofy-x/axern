package runtime

import (
	"context"
	"errors"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func runtimeContainerAbsent(err error, containerID string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errord.ErrNotFound) || errord.IsNotFound(errord.FromGRPC(err)) {
		return true
	}
	return ocicli.IsContainerNotFound(err, containerID)
}

// nativeRuntimeContainerAbsent combines the exact state command's not-found
// response with a no-follow lookup in the trusted native runsc root. The pinned
// release omits the container ID from FetchSpec errors, so that diagnostic alone
// must never authorize cleanup of a bundle or its resource claims.
func (r *RunscServiceHandler) nativeRuntimeContainerAbsent(ctx context.Context, containerID string, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	recognized := runtimeContainerAbsent(err, containerID)
	if !recognized {
		var commandErr *ocicli.CommandError
		recognized = errors.As(err, &commandErr) && strings.TrimSpace(commandErr.Output) == "FetchSpec failed: loading container: file does not exist"
	}
	if !recognized {
		return false, nil
	}
	return nativeRuntimeStateAbsent(ctx, r.common.RuntimeRoot(), containerID)
}
