package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/require"
)

func TestRunReturnsWithoutBlocking(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Shutdown(ctx))
	})

	done := make(chan error, 1)
	go func() {
		done <- s.Run(t.Context())
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run blocked instead of returning after starting background loops")
	}
}

func TestRunMarksServiceReadyAfterInitialHousekeeping(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})
	s.ready.Store(false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Shutdown(ctx))
	})

	require.NoError(t, s.Run(t.Context()))

	require.Eventually(t, s.Ready, 2*time.Second, 50*time.Millisecond)
}

func TestActiveAllocationIDsNormalizesForVolumeReconcile(t *testing.T) {
	got := activeAllocationIDs([]*container.Container{
		nil,
		{Metadata: nil},
		{Metadata: &runtimeapi.ContainerMetadata{ID: " alloc-b "}},
		{Metadata: &runtimeapi.ContainerMetadata{ID: ""}},
		{Metadata: &runtimeapi.ContainerMetadata{ID: "alloc-a"}},
		{Metadata: &runtimeapi.ContainerMetadata{ID: "alloc-b"}},
	})
	want := []string{"alloc-a", "alloc-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activeAllocationIDs() = %#v, want %#v", got, want)
	}
}

func TestShutdownDrainsRetainedRuntimes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires linux resource manager setup")
	}

	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	require.NoError(t, os.MkdirAll(rootfsDir, 0o755))

	fr := &runtimeapi.RuntimeTemplate{
		ID:      "retained-on-close",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sh"},
	}
	rootfsCfg, err := langruntime.RootfsConfigFromRuntimeTemplate(fr)
	require.NoError(t, err)
	result, err := s.lrtManager.AddLangRuntime(t.Context(), fr, rootfsCfg, true)
	require.NoError(t, err)
	lr := result.Runtime

	lr.IncRef()
	lr.DecRef()
	require.True(t, lr.Retained())

	require.NoError(t, s.Shutdown(t.Context()))
	require.Nil(t, s.lrtManager.GetLangRuntime("retained-on-close"))
	require.True(t, lr.Released())
}
