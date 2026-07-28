package allocation

import (
	"fmt"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"google.golang.org/protobuf/proto"
)

type testStateStore interface {
	stateStore
	SaveSnapshot(bucket string, value proto.Message) error
	LoadSnapshot(bucket string, value proto.Message) error
	GetRecord(bucket, key string, value proto.Message) error
}

type testAllocationController struct {
	controller *Controller
	manager    *container.Manager
	lrtManager *langrtmanager.LangRTManager
}

func newTestAllocationController(t *testing.T, handlers map[string]contract.RuntimeHandler) testAllocationController {
	t.Helper()
	return newTestAllocationControllerWithStore(t, handlers, storetest.NewMockStore(), fakeVolumePublisher{})
}

func newTestAllocationControllerWithStore(t *testing.T, handlers map[string]contract.RuntimeHandler, dbStore testStateStore, publisher fakeVolumePublisher) testAllocationController {
	t.Helper()

	if dbStore == nil {
		dbStore = storetest.NewMockStore()
	}
	tmpDir := t.TempDir()
	runtimeBinary := make(map[string]string)
	runtimes := make(map[string]config.RuntimeInstanceConfig)
	for name := range handlers {
		runtimeBinary[name] = "/fake/" + name
		runtimes[name] = config.RuntimeInstanceConfig{Binary: "/fake/" + name}
	}
	cfg := config.Config{
		RootDir: tmpDir,
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{
				Runtimes:      runtimes,
				RuntimeBinary: runtimeBinary,
			},
		},
	}
	registry := handlerregistry.New(cfg)
	for name, h := range handlers {
		registry.Set(name, h)
	}
	manager, err := container.NewManager(tmpDir, registry.Map(), make(chan bool, 10), newTestResourceManagers()...)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	lrtManager := langrtmanager.NewLanguageRuntimeManager()
	volumes := servicevolumes.NewCoordinator(servicevolumes.Options{
		Publisher: publisher,
		ActiveAllocationIDs: func() []string {
			return activeAllocationIDs(manager.List())
		},
	})
	networking := servicenetworking.NewCoordinator(servicenetworking.Options{
		NatBackend: cfg.NatBackend,
		Store:      dbStore,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return manager.CollectResourceByID(id)
		},
		ContainerExists: func(id string) bool {
			_, err := manager.Get(id)
			return err == nil
		},
		RuntimeClass: func(id string) (string, error) {
			c, err := manager.Get(id)
			if err != nil || c == nil || c.Metadata == nil {
				return "", err
			}
			return c.Metadata.GetRuntimeHandler(), nil
		},
	})
	controller := NewController(Options{
		Config: cfg,
		Store:  dbStore,
		ContainerManager: func() *container.Manager {
			return manager
		},
		RuntimeHandler: func(name string) (contract.RuntimeHandler, error) {
			if handler, ok := handlers[name]; ok {
				return handler, nil
			}
			return nil, fmt.Errorf("runtime %s is not supported", name)
		},
		LangRuntime: lrtManager,
		Volumes:     volumes,
		Networking:  networking,
	})
	retentionTTL, err := time.ParseDuration(config.DefaultIdleRuntimeRetentionTTL)
	if err != nil {
		t.Fatalf("ParseDuration() error = %v", err)
	}
	lrtManager.ConfigureRetention(retentionTTL, config.DefaultIdleRuntimeRetentionMax)
	return testAllocationController{controller: controller, manager: manager, lrtManager: lrtManager}
}
