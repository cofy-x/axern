package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRunscHandlerWaitReturnsUnavailableWhenRuntimeContainerIsAbsent(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{
		"wait": {&ocicli.CommandError{Err: errors.New("exit status 128"), Output: "error: container axctl-test not found"}},
	}})

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
	assert.Equal(t, -1, exit.Status)
}

func runscWaitGraceAttrs(result string) map[string]string {
	return map[string]string{
		sdkobs.AttrRuntime: config.RuntimeNameRunsc,
		sdkobs.AttrResult:  result,
	}
}

func TestRunscHandlerWaitReturnsUnavailableWhenStoppedWithoutExitState(t *testing.T) {
	metrics.ResetForTest()

	previousGrace := runscExitStateGracePeriod
	previousRetryTimeout := runscWaitRetryTimeout
	runscExitStateGracePeriod = 300 * time.Millisecond
	runscWaitRetryTimeout = 25 * time.Millisecond
	defer func() {
		runscExitStateGracePeriod = previousGrace
		runscWaitRetryTimeout = previousRetryTimeout
	}()

	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	stateOutputs := make([][]byte, 0, 16)
	stateOutputs = append(stateOutputs, []byte(`{"status":"running"}`))
	for i := 0; i < 15; i++ {
		stateOutputs = append(stateOutputs, []byte(`{"status":"stopped"}`))
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"state": stateOutputs,
		},
		errors: map[string][]error{
			"wait": {
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	graceUnavailableAttrs := runscWaitGraceAttrs("unavailable")
	beforeGraceUnavailable := metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceUnavailableAttrs)
	exit, err := handler.Wait(ctx, contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.Error(t, err)
	assert.True(t, contract.IsExitStatusUnavailable(err))
	assert.Equal(t, -1, exit.Status)
	assert.Equal(t, beforeGraceUnavailable+1, metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceUnavailableAttrs))
}

func TestRunscHandlerWaitAcceptsDelayedPersistedExitState(t *testing.T) {
	metrics.ResetForTest()

	previousGrace := runscExitStateGracePeriod
	previousRetryTimeout := runscWaitRetryTimeout
	runscExitStateGracePeriod = 500 * time.Millisecond
	runscWaitRetryTimeout = 25 * time.Millisecond
	defer func() {
		runscExitStateGracePeriod = previousGrace
		runscWaitRetryTimeout = previousRetryTimeout
	}()

	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}

	stateOutputs := make([][]byte, 0, 16)
	stateOutputs = append(stateOutputs, []byte(`{"status":"running"}`))
	for i := 0; i < 15; i++ {
		stateOutputs = append(stateOutputs, []byte(`{"status":"stopped"}`))
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"state": stateOutputs,
		},
		errors: map[string][]error{
			"wait": {
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
				errors.New("wait failed"),
			},
		},
	})

	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = handler.common.PersistExitState("axctl-test", contract.Exit{
			Timestamp: time.Now(),
			Status:    17,
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	graceRecoveredAttrs := runscWaitGraceAttrs("recovered")
	beforeGraceRecovered := metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs)
	exit, err := handler.Wait(ctx, contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 17, exit.Status)
	assert.Equal(t, beforeGraceRecovered+1, metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs))
}

func TestRunscHandlerWaitRetriesOCIWaitAfterStop(t *testing.T) {
	metrics.ResetForTest()

	previousGrace := runscExitStateGracePeriod
	previousRetryTimeout := runscWaitRetryTimeout
	runscExitStateGracePeriod = 500 * time.Millisecond
	runscWaitRetryTimeout = 25 * time.Millisecond
	defer func() {
		runscExitStateGracePeriod = previousGrace
		runscWaitRetryTimeout = previousRetryTimeout
	}()

	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&retryingWaitExecutor{})

	graceRecoveredAttrs := runscWaitGraceAttrs("recovered")
	beforeGraceRecovered := metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs)
	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 29, exit.Status)
	assert.Equal(t, beforeGraceRecovered+1, metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs))
}

func TestRunscHandlerWaitUsesOCIWaitOutput(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"wait": {[]byte(`{"status":23}`)},
		},
	})

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 23, exit.Status)

	persisted, ok, err := handler.readExitState("axctl-test")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 23, persisted.Status)
}

func TestRunscHandlerWaitIgnoresTransientExitWhileStateStillRunning(t *testing.T) {
	metrics.ResetForTest()

	previousGrace := runscExitStateGracePeriod
	previousRetryTimeout := runscWaitRetryTimeout
	runscExitStateGracePeriod = 500 * time.Millisecond
	runscWaitRetryTimeout = 25 * time.Millisecond
	defer func() {
		runscExitStateGracePeriod = previousGrace
		runscWaitRetryTimeout = previousRetryTimeout
	}()

	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"wait": {
				[]byte(`{"status":128}`),
				[]byte(`{"status":29}`),
			},
			"state": {
				[]byte(`{"status":"running"}`),
				[]byte(`{"status":"stopped"}`),
			},
		},
	})

	graceRecoveredAttrs := runscWaitGraceAttrs("recovered")
	beforeGraceRecovered := metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs)
	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 29, exit.Status)
	assert.Equal(t, beforeGraceRecovered+1, metrics.CounterValueForTest(metrics.MetricRuntimeWaitGraceTotal, graceRecoveredAttrs))

	persisted, ok, err := handler.readExitState("axctl-test")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 29, persisted.Status)
}

func TestRunscHandlerExitStatePersisterIgnoresTransientExitWhileStateStillRunning(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"wait": {
				[]byte(`{"status":128}`),
				[]byte(`{"status":29}`),
			},
			"state": {
				[]byte(`{"status":"running"}`),
				[]byte(`{"status":"stopped"}`),
			},
		},
	})

	if err := handler.startExitStatePersister("axctl-test"); err != nil {
		t.Fatalf("startExitStatePersister() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		persisted, ok, err := handler.readExitState("axctl-test")
		if err != nil {
			t.Fatalf("readExitState() error = %v", err)
		}
		if ok {
			assert.Equal(t, 29, persisted.Status)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("timed out waiting for exit-state persister to persist exit state")
}
