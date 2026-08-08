package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestRuntimeDeleteAbsentStillCompletesOwnedCleanup(t *testing.T) {
	handler, err := NewRuncServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{"": {errors.New("container does not exist")}}})
	assertDeleteRemovesExitState(t, handler.common.RuntimeExitStatePath("alloc-a"), func() error {
		_, err := handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
		return err
	}, handler.persistExitState)
}

func TestRunscDeleteRemovesPersistedExitState(t *testing.T) {
	handler, err := NewRunscServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunsc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&recordingExecutor{})
	assertDeleteRemovesExitState(t, handler.common.RuntimeExitStatePath("alloc-a"), func() error {
		_, err := handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
		return err
	}, handler.persistExitState)
}

func TestRuncDeleteRemovesPersistedExitState(t *testing.T) {
	handler, err := NewRuncServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetExecutor(&recordingExecutor{})
	assertDeleteRemovesExitState(t, handler.common.RuntimeExitStatePath("alloc-a"), func() error {
		_, err := handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
		return err
	}, handler.persistExitState)
}

func assertDeleteRemovesExitState(
	t *testing.T,
	path string,
	remove func() error,
	persist func(string, contract.Exit) error,
) {
	t.Helper()
	if err := persist("alloc-a", contract.Exit{Timestamp: time.Now().UTC(), Status: 0}); err != nil {
		t.Fatalf("persistExitState() error = %v", err)
	}
	if err := remove(); err != nil {
		t.Fatalf("DeleteContainer() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("exit state stat error = %v, want not exist", err)
	}
}
