package runtime

import (
	"context"
	"encoding/json"
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
	if err := handler.persistExitState("alloc-a", contract.Exit{Timestamp: time.Now().UTC(), Status: 137}); err != nil {
		t.Fatal(err)
	}

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

func TestRunscForceDeleteWaitsForForegroundExitState(t *testing.T) {
	handler, err := NewRunscServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunsc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	done := make(chan error, 1)
	go func() {
		_, err := handler.DeleteContainer(context.Background(), &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
		done <- err
	}()

	deadline := time.Now().Add(time.Second)
	for len(recorder.Args()) != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := recorder.Args(); len(got) != 1 || !containsArg(got[0], "kill") {
		t.Fatalf("commands before exit state = %#v, want only kill", got)
	}
	select {
	case err := <-done:
		t.Fatalf("DeleteContainer() returned before runtime runner exit: %v", err)
	default:
	}
	if err := handler.persistExitState("alloc-a", contract.Exit{Timestamp: time.Now().UTC(), Status: 137}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteContainer() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeleteContainer() did not continue after exit state was persisted")
	}
	if got := recorder.Args(); len(got) != 2 || !containsArg(got[1], "delete") {
		t.Fatalf("commands after exit state = %#v, want kill then delete", got)
	}
}

func TestRunscForceDeleteDoesNotDeleteBeforeForegroundExit(t *testing.T) {
	handler, err := NewRunscServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunsc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = handler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DeleteContainer() error = %v, want deadline exceeded", err)
	}
	if got := recorder.Args(); len(got) != 1 || !containsArg(got[0], "kill") {
		t.Fatalf("commands without foreground exit = %#v, want only kill", got)
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

func TestRuncDeleteWaitsForInitMonitorBeforeReleasingOwnedStorage(t *testing.T) {
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
	readyPath := handler.common.InitMonitorReadyStatePath("alloc-a")
	if err := os.MkdirAll(filepath.Dir(readyPath), 0755); err != nil {
		t.Fatal(err)
	}
	readyPayload, err := json.Marshal(map[string]any{
		"ready": true, "initPid": 321, "observedAt": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyPath, readyPayload, 0644); err != nil {
		t.Fatal(err)
	}

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()
	done := make(chan error, 1)
	go func() {
		_, err := handler.DeleteContainer(callerCtx, &apipb.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{ContainerID: "alloc-a"})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("DeleteContainer() returned before init monitor persisted exit state: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !hasWritableReservation(manager, "alloc-a") {
		t.Fatal("writable reservation released before init monitor persistence barrier")
	}
	if err := handler.persistExitState("alloc-a", contract.Exit{Timestamp: time.Now().UTC(), Status: 137}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteContainer() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeleteContainer() did not continue after init monitor persistence")
	}
	if hasWritableReservation(manager, "alloc-a") {
		t.Fatal("writable reservation retained after ordered delete completed")
	}
	if _, err := os.Stat(readyPath); !os.IsNotExist(err) {
		t.Fatalf("monitor ready state stat error = %v, want not exist", err)
	}
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
