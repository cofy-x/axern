package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
)

func (r *RuncServiceHandler) state(ctx context.Context, containerID string) (runcState, error) {
	output, err := r.common.Run(ctx, "state", containerID)
	if err != nil {
		return runcState{}, err
	}

	var state runcState
	if err := ocihost.DecodeState("runc", output, &state); err != nil {
		return runcState{}, err
	}
	return state, nil
}
