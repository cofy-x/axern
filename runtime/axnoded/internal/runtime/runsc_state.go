package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
)

func (r *RunscServiceHandler) state(ctx context.Context, containerID string) (runscState, error) {
	output, err := r.runLifecycle(ctx, "state", containerID)
	if err != nil {
		return runscState{}, err
	}

	var state runscState
	if err := ocihost.DecodeState("runsc", output, &state); err != nil {
		return runscState{}, err
	}
	return state, nil
}
