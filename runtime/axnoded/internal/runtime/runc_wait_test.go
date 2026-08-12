package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRuncHandlerWaitReturnsUnavailableWhenRuntimeContainerIsAbsent(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{
		"state": {&ocicli.CommandError{Err: errors.New("exit status 1"), Output: `container "axctl-test" does not exist`}},
	}})

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
	assert.Equal(t, -1, exit.Status)
}

func TestRuncHandlerWaitAcceptsScopedAbsentOutputWithoutContainerID(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{
		"state": {&ocicli.CommandError{Err: errors.New("exit status 1"), Output: `time="2026-08-12T17:18:02Z" level=error msg="container does not exist"`}},
	}})

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
	assert.Equal(t, -1, exit.Status)
}

func TestRuncHandlerWaitNeverInventsZeroExitFromStoppedState(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"state": {
				[]byte(`{"status":"running"}`),
				[]byte(`{"status":"stopped"}`),
				[]byte(`{"status":"stopped"}`),
				[]byte(`{"status":"stopped"}`),
			},
		},
	})
	previousGrace := runcExitStateGracePeriod
	runcExitStateGracePeriod = 150 * time.Millisecond
	t.Cleanup(func() { runcExitStateGracePeriod = previousGrace })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	exit, err := handler.Wait(ctx, contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
	assert.Equal(t, -1, exit.Status)
}

func TestRuncHandlerWaitPrefersPersistedExitState(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "containers", "axctl-test"), 0755); err != nil {
		t.Fatalf("mkdir container root: %v", err)
	}

	finishedAt := time.Now().UTC().Round(time.Second)
	payload := []byte(fmt.Sprintf(`{"exitCode":17,"finishedAt":%q}`, finishedAt.Format(time.RFC3339)))
	if err := os.MkdirAll(filepath.Dir(handler.common.RuntimeExitStatePath("axctl-test")), 0755); err != nil {
		t.Fatalf("mkdir exit state dir: %v", err)
	}
	if err := os.WriteFile(handler.common.RuntimeExitStatePath("axctl-test"), payload, 0644); err != nil {
		t.Fatalf("write exit state: %v", err)
	}

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 17, exit.Status)
	assert.Equal(t, finishedAt, exit.Timestamp)
}

func TestRuncHandlerWaitAcceptsInitMonitorExitAfterStoppedState(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "containers", "axctl-test"), 0755); err != nil {
		t.Fatalf("mkdir container root: %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{
			"state": {
				[]byte(`{"status":"stopped"}`),
				[]byte(`{"status":"stopped"}`),
				[]byte(`{"status":"stopped"}`),
			},
		},
	})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = handler.persistExitState("axctl-test", contract.Exit{Timestamp: time.Now().UTC(), Status: 9})
	}()

	exit, err := handler.Wait(context.Background(), contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	assert.Equal(t, 9, exit.Status)

	persisted, ok, err := handler.readExitState("axctl-test")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 9, persisted.Status)
}

func TestRuncHandlerWaitReturnsContextErrorWhenCanceled(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&scriptedExecutor{
		outputs: map[string][][]byte{"state": {[]byte(`{"status":"running"}`)}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = handler.Wait(ctx, contract.HandlerOptions{ContainerID: "axctl-test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context canceled", err)
	}
}
