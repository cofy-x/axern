//go:build linux

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/require"
)

func TestNativeConformanceCleanupWithoutMemoryIdentityNeverSignals(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "still-live", true: "kernel-stopped"}[stopped], func(t *testing.T) {
			h, manifest, executor := taskEnforcementFixture(t)
			h.nativeRecovery = true
			id := "task-allocation"
			root := h.common.RuntimeRoot()
			f := nativeForgetFixture{
				root: root, id: id, bundle: manifest.BundlePath,
				state: filepath.Join(root, id+"_sandbox:"+id+".state"),
				lock:  filepath.Join(root, id+"_sandbox:"+id+".lock"),
				value: map[string]any{
					"id": id, "bundleDir": manifest.BundlePath, "status": "running", "goferPid": os.Getpid(),
					"sandbox": map[string]any{"id": id, "pid": 1234, "controlSocketPath": filepath.Join(root, "runsc-"+id+".sock")},
					"saver":   map[string]any{"rootDir": root, "id": map[string]any{"sandboxId": id, "containerId": id}},
				},
			}
			f.write(t)
			require.NoError(t, os.WriteFile(f.lock, nil, 0600))
			previous, previousKill := conformanceCgroupStopped, killRecoveryCgroup
			t.Cleanup(func() { conformanceCgroupStopped, killRecoveryCgroup = previous, previousKill })
			proofCalls := 0
			conformanceCgroupStopped = func(path string, pid int) (bool, error) {
				proofCalls++
				require.Equal(t, manifest.CgroupPath, path, "all descendants of the owned parent must be empty")
				require.Equal(t, 1234, pid)
				return stopped, nil
			}
			killRecoveryCgroup = func(string, int, string, string, string, uint64) error {
				t.Fatal("legacy cleanup must never issue a kernel kill")
				return nil
			}
			options := contract.HandlerOptions{ContainerID: id, EnforcementManifest: manifest, RuntimeConformanceCleanup: true}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			err := h.deleteNativeRuntimeContainer(ctx, options, true)
			if stopped {
				require.NoError(t, err)
				require.NoFileExists(t, f.state)
				require.NoFileExists(t, f.lock)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.FileExists(t, f.state)
				require.FileExists(t, f.lock)
			}
			require.Positive(t, proofCalls)
			for _, args := range executor.Args() {
				require.NotContains(t, args, "kill")
				require.NotContains(t, args, "delete")
				require.NotContains(t, args, "resume")
			}
		})
	}
}

func TestMemorylessWorkloadCannotUseConformanceCleanupImplicitly(t *testing.T) {
	h, manifest, _ := taskEnforcementFixture(t)
	h.nativeRecovery = true
	previous := conformanceCgroupStopped
	t.Cleanup(func() { conformanceCgroupStopped = previous })
	conformanceCgroupStopped = func(string, int) (bool, error) {
		t.Fatal("missing typed conformance ownership must not use the legacy exception")
		return true, nil
	}
	require.ErrorContains(t, h.deleteNativeRuntimeContainer(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: manifest}, true), "requires complete identity")
}
