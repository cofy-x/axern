package service

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
)

// newTestService creates a sandboxService with a real container.Manager backed by a temp dir.
func newTestService(t *testing.T, handlers map[string]contract.RuntimeHandler) *sandboxService {
	lrtManager := langrtmanager.NewLanguageRuntimeManager()
	retentionTTL, err := time.ParseDuration(config.DefaultIdleRuntimeRetentionTTL)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	lrtManager.ConfigureRetention(retentionTTL, config.DefaultIdleRuntimeRetentionMax)
	return newTestServiceWithLanguageRuntimeManager(t, handlers, lrtManager)
}

func newTestServiceWithLanguageRuntimeManager(t *testing.T, handlers map[string]contract.RuntimeHandler, lrtManager *langrtmanager.LangRTManager) *sandboxService {
	t.Helper()
	if lrtManager == nil {
		t.Fatal("language runtime manager is required")
	}

	tmpDir := t.TempDir()

	registry := handlerregistry.New(config.Config{
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{Runsc: config.RuntimeInstanceConfig{Binary: "/fake/runsc"}},
		},
	})
	for name, h := range handlers {
		registry.Set(name, h)
	}

	healthChan := make(chan bool, 10)

	cm, err := container.NewManager(tmpDir, registry.Map(), healthChan, newTestResourceManagers()...)
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
		runtimeHandlers:  registry,
		containerManager: cm,
		store:            storetest.NewMockStore(),
		lrtManager:       lrtManager,
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
