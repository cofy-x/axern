package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workloadClientStub struct {
	signal     string
	stopSignal string
	stop       wire.WorkloadExitResponse
	wait       wire.WorkloadExitResponse
	err        error
}

func (c *workloadClientStub) SignalWorkload(_ context.Context, signal string) error {
	c.signal = signal
	return c.err
}

func (c *workloadClientStub) StopWorkload(_ context.Context, signal string) (wire.WorkloadExitResponse, error) {
	c.stopSignal = signal
	return c.stop, c.err
}

func (c *workloadClientStub) WaitWorkload(context.Context) (wire.WorkloadExitResponse, error) {
	return c.wait, c.err
}

func newRunscWorkloadTestHandler(t *testing.T, client *workloadClientStub) *RunscServiceHandler {
	t.Helper()
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	require.NoError(t, err)
	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	require.NoError(t, err)
	handler.newWorkloadClient = func(string) sandboxdWorkloadClient { return client }
	return handler
}

func TestRunscHandlerWaitUsesSandboxdWorkloadResult(t *testing.T) {
	exitedAt := time.Now().UTC().Truncate(time.Nanosecond)
	handler := newRunscWorkloadTestHandler(t, &workloadClientStub{wait: wire.WorkloadExitResponse{ExitCode: 23, ExitedAt: exitedAt}})
	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "allocation-one"})
	require.NoError(t, err)
	assert.Equal(t, 23, exit.Status)
	assert.Equal(t, exitedAt, exit.Timestamp)
}

func TestRunscHandlerWaitFailsClosedWhenSandboxdResultUnavailable(t *testing.T) {
	handler := newRunscWorkloadTestHandler(t, &workloadClientStub{err: errors.New("socket unavailable")})
	_, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "allocation-one"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
}

func TestRunscHandlerKillSignalsSupervisedWorkloadWithoutOCIKill(t *testing.T) {
	client := &workloadClientStub{}
	handler := newRunscWorkloadTestHandler(t, client)
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	_, err := handler.KillContainer(context.Background(), &apipb.SignalContainerRequest{ID: "allocation-one", Signal: "KILL"}, contract.HandlerOptions{ContainerID: "allocation-one"})
	require.NoError(t, err)
	assert.Equal(t, "KILL", client.signal)
	assert.Empty(t, recorder.Args())
}

func TestRunscHandlerStopWaitsForSupervisedWorkloadWithoutOCIKill(t *testing.T) {
	client := &workloadClientStub{stop: wire.WorkloadExitResponse{ExitCode: 137, ExitedAt: time.Now().UTC()}}
	handler := newRunscWorkloadTestHandler(t, client)
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	exit, err := handler.StopWorkload(context.Background(), contract.HandlerOptions{ContainerID: "allocation-one"})
	require.NoError(t, err)
	assert.Equal(t, "KILL", client.stopSignal)
	assert.Equal(t, 137, exit.Status)
	assert.Empty(t, recorder.Args())
}

func TestRunscHandlerRejectsWorkloadResultWithoutExitTimestamp(t *testing.T) {
	handler := newRunscWorkloadTestHandler(t, &workloadClientStub{wait: wire.WorkloadExitResponse{ExitCode: 0}})
	_, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "allocation-one"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
}

func TestRunscHandlerExitStatePersisterIgnoresTransientExitWhileStateStillRunning(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	require.NoError(t, err)
	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	require.NoError(t, err)
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"wait":  {[]byte(`{"status":128}`), []byte(`{"status":29}`)},
			"state": {[]byte(`{"status":"running"}`), []byte(`{"status":"stopped"}`)},
		},
	})
	require.NoError(t, handler.startExitStatePersister("allocation-one"))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		persisted, ok, readErr := handler.readExitState("allocation-one")
		require.NoError(t, readErr)
		if ok {
			assert.Equal(t, 29, persisted.Status)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for OCI cleanup exit-state persistence")
}
