package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
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
	handler.common.SetExecutor(&scriptedExecutor{errors: map[string][]error{"": {&ocicli.CommandError{Err: errors.New("exit status 1"), Output: "container alloc-a does not exist"}}}})
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

func TestRunscForceDeleteStopsForegroundRunBeforeDeletingState(t *testing.T) {
	rootDir := t.TempDir()
	handler, err := NewRunscServiceHandler(
		config.Config{RootDir: rootDir},
		config.RuntimeNameRunsc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)

	if _, err := handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"}); err != nil {
		t.Fatalf("DeleteContainer() error = %v", err)
	}

	want := [][]string{
		{"--root", filepath.Join(rootDir, config.RuntimeNameRunsc), "--allow-suid", "kill", "alloc-a", "KILL"},
		{"--root", filepath.Join(rootDir, config.RuntimeNameRunsc), "--allow-suid", "delete", "--force", "alloc-a"},
	}
	if got := recorder.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime commands = %#v, want %#v", got, want)
	}
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

func TestDeleteReleasesWritableReservationWhenExitStateRemovalFails(t *testing.T) {
	rootDir := t.TempDir()
	handler, err := NewRuncServiceHandler(
		config.Config{RootDir: rootDir},
		config.RuntimeNameRunc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	filestore := t.TempDir()
	manager, err := sharedWritableCapacityManager(filestore, 0)
	if err != nil {
		t.Fatal(err)
	}
	handler.writableCapacity = manager
	if err := manager.Reserve("alloc-a", config.RuntimeNameRunc, 1, 1); err != nil {
		t.Fatal(err)
	}
	handler.common.SetExecutor(&recordingExecutor{})
	exitStatePath := handler.common.RuntimeExitStatePath("alloc-a")
	if err := os.MkdirAll(filepath.Join(exitStatePath, "non-empty"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err = handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
	if err == nil {
		t.Fatal("expected exit-state removal error")
	}
	if hasWritableReservation(manager, "alloc-a") {
		t.Fatal("writable reservation must be released after rootfs cleanup succeeds")
	}
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
