package service

import (
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
	t.Helper()

	tmpDir := t.TempDir()

	runtimeBinary := make(map[string]string)
	runtimes := make(map[string]config.RuntimeInstanceConfig)
	for name := range handlers {
		runtimeBinary[name] = "/fake/" + name
		runtimes[name] = config.RuntimeInstanceConfig{Binary: "/fake/" + name}
	}
	registry := handlerregistry.New(config.Config{
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{
				Runtimes: runtimes,
			},
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

	s := &sandboxService{
		config: config.Config{
			RootDir: tmpDir,
			PluginConfig: config.PluginConfig{
				RuntimeConfig: config.RuntimeConfig{
					Runtimes:      runtimes,
					RuntimeBinary: runtimeBinary,
				},
			},
		},
		runtimeHandlers:  registry,
		containerManager: cm,
		store:            storetest.NewMockStore(),
		lrtManager:       langrtmanager.NewLanguageRuntimeManager(),
		volumeClient:     fakeVolumePublisher{},
	}
	s.configureProbeCoordinator()
	s.configureVolumeCoordinator()
	s.configureSandboxTargets()
	s.configureSandboxAccess()
	s.configureNetworking()
	s.configureProcessController()
	s.configureSandboxControl()
	s.configureControlPlaneReports()
	s.configureAllocationController()
	cm.SetExitObserver(s.handleContainerExitControlPlaneReport)
	retentionTTL, err := time.ParseDuration(config.DefaultIdleRuntimeRetentionTTL)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	s.lrtManager.ConfigureRetention(retentionTTL, config.DefaultIdleRuntimeRetentionMax)
	go func() {
		for ready := range healthChan {
			s.ready.Store(ready)
		}
	}()
	s.ready.Store(true)
	return s
}
