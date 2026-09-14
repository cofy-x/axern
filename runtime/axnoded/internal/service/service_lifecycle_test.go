package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/require"
)

func TestRunReturnsWithoutBlocking(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)
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
	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)
	s.ready.Store(false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Shutdown(ctx))
	})

	require.NoError(t, s.Run(t.Context()))

	require.Eventually(t, s.Ready, 2*time.Second, 50*time.Millisecond)
}

func TestShutdownDrainsRetainedEnvironments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires linux resource manager setup")
	}

	s := newTestService(t,
		runtimetest.NewFakeSandboxRuntime(),
	)

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	require.NoError(t, os.MkdirAll(rootfsDir, 0o755))

	fr := &runtimeapi.ResolvedEnvironment{
		ID: "retained-on-close",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
		},
		Argv: []string{"/bin/sh"},
	}
	rootfsCfg, err := environmentcache.RootfsConfigFromResolvedEnvironment(fr)
	require.NoError(t, err)
	result, err := s.environmentCache.PrepareEnvironment(t.Context(), fr, rootfsCfg)
	require.NoError(t, err)
	lr := result.Environment

	lr.IncRef()
	lr.DecRef()
	require.True(t, lr.Retained())

	require.NoError(t, s.Shutdown(t.Context()))
	require.Nil(t, s.environmentCache.GetPreparedEnvironment("retained-on-close"))
	require.True(t, lr.Released())
}
