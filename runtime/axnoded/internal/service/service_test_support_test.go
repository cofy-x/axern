package service

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
)

// newTestService creates a sandboxService with a real container.Manager backed by a temp dir.
func newTestService(t *testing.T, runscHandler contract.SandboxRuntime) *sandboxService {
	environmentCache := environmentcache.NewEnvironmentCache()
	retentionTTL, err := time.ParseDuration(config.DefaultIdleEnvironmentRetentionTTL)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	environmentCache.ConfigureRetention(retentionTTL, config.DefaultIdleEnvironmentRetentionMax)
	return newTestServiceWithPreparedEnvironmentManager(t, runscHandler, environmentCache)
}

func newTestServiceWithPreparedEnvironmentManager(t *testing.T, runscHandler contract.SandboxRuntime, environmentCache *environmentcache.EnvironmentCache) *sandboxService {
	t.Helper()
	if environmentCache == nil {
		t.Fatal("environment cache is required")
	}

	tmpDir := t.TempDir()

	managerHandler := runscHandler
	if managerHandler == nil {
		managerHandler = runtimetest.NewFakeSandboxRuntime()
	}

	healthChan := make(chan bool, 10)

	cm, err := container.NewManager(tmpDir, managerHandler, healthChan, newTestResourceManagers()...)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.NoError(t, cm.Stop(ctx))
	})

	s := &sandboxService{
		config: config.Config{
			RootDir: tmpDir,
			PluginConfig: config.PluginConfig{
				RuntimeConfig: config.RuntimeConfig{Runsc: config.RuntimeInstanceConfig{Binary: "/fake/runsc"}},
			},
		},
		runscHandler:     runscHandler,
		containerManager: cm,
		store:            storetest.NewMockStore(),
		environmentCache: environmentCache,
	}
	s.configureSandboxTargets()
	s.configureSandboxAccess()
	s.configureNetworking()
	s.configureProcessController()
	s.configureSandboxControl()
	s.configureControlPlaneReports()
	s.configureAllocationController()
	cm.SetExitClassifier(s.classifyContainerExit)
	cm.SetExitObserver(s.handleContainerExitControlPlaneReport)
	go func() {
		for ready := range healthChan {
			s.ready.Store(ready)
		}
	}()
	s.ready.Store(true)
	return s
}
