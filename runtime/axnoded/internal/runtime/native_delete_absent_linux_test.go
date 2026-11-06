//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	"github.com/stretchr/testify/require"
)

type nativeMissingStateExecutor struct {
	recordingExecutor
	err error
}

func (e *nativeMissingStateExecutor) Execute(ctx context.Context, binary string, args ...string) ([]byte, error) {
	_, _ = e.recordingExecutor.Execute(ctx, binary, args...)
	return nil, e.err
}

func TestNativeAbsenceNeedsScopedRuntimeFailureAndExactMissingState(t *testing.T) {
	const pinnedMissing = "FetchSpec failed: loading container: file does not exist"
	for _, test := range []struct {
		name, output string
		stateExists  bool
		wantAbsent   bool
	}{
		{"pinned missing", pinnedMissing, false, true},
		{"pinned missing with state", pinnedMissing, true, false},
		{"exact ID missing", "container allocation does not exist", false, true},
		{"exact ID missing with state", "container allocation does not exist", true, false},
		{"different ID", "container another does not exist", false, false},
		{"generic filesystem failure", "file does not exist", false, false},
		{"runtime unavailable", "exec /usr/local/bin/runsc: no such file or directory", false, false},
		{"missing rootfs", "FetchSpec failed: rootfs: file does not exist", false, false},
		{"additional runtime failure", pinnedMissing + "\nadditional failure", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newNativeRecoveryTestHandler(t)
			if test.stateExists {
				require.NoError(t, os.WriteFile(filepath.Join(h.common.RuntimeRoot(), "allocation_sandbox:allocation.state"), []byte("retained state"), 0600))
			}
			err := &ocicli.CommandError{Err: errors.New("exit status 128"), Output: test.output}
			absent, proofErr := h.nativeRuntimeContainerAbsent(t.Context(), "allocation", err)
			require.NoError(t, proofErr)
			require.Equal(t, test.wantAbsent, absent)
			e := &nativeMissingStateExecutor{err: err}
			h.common.SetExecutor(e)
			deleteErr := h.deleteRuntimeContainer(t.Context(), "allocation", true)
			if test.wantAbsent {
				require.NoError(t, deleteErr)
			} else {
				require.Error(t, deleteErr)
			}
			for _, args := range e.Args() {
				require.Contains(t, args, "state")
				require.NotContains(t, args, "kill")
				require.NotContains(t, args, "delete")
			}
		})
	}
	// The generic matcher remains unchanged for Compose and local operation.
	require.False(t, runtimeContainerAbsent(&ocicli.CommandError{Err: errors.New("exit status 128"), Output: pinnedMissing}, "allocation"))
}
