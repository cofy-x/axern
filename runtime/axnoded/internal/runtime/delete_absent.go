package runtime

import (
	"errors"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func runtimeDeleteTargetAbsent(err error, containerID string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errord.ErrNotFound) || errord.IsNotFound(errord.FromGRPC(err)) {
		return true
	}
	return ocicli.IsContainerNotFound(err, containerID)
}
