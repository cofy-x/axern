package allocation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
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
	return newTestAllocationControllerWithStore(t, handlers, storetest.NewMockStore())
}

func newTestAllocationControllerWithStore(t *testing.T, handlers map[string]contract.RuntimeHandler, dbStore testStateStore) testAllocationController {
	return newTestAllocationControllerWithResources(t, handlers, dbStore, newTestResourceManagers()...)
}

func newTestAllocationControllerWithResources(t *testing.T, handlers map[string]contract.RuntimeHandler, dbStore testStateStore, managers ...resourcemanager.Manager) testAllocationController {
	t.Helper()

	if dbStore == nil {
		dbStore = storetest.NewMockStore()
	}
	tmpDir := t.TempDir()
	cfg := config.Config{
		RootDir: tmpDir,
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{Runsc: config.RuntimeInstanceConfig{Binary: "/fake/runsc"}},
		},
	}
	registry := handlerregistry.New(cfg)
	for name, h := range handlers {
		registry.Set(name, h)
	}
	manager, err := container.NewManager(tmpDir, registry.Map(), make(chan bool, 10), managers...)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Errorf("stop test container manager: %v", err)
		}
	})
	lrtManager := langrtmanager.NewLanguageRuntimeManager()
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
		Networking:  networking,
		PreActivationCapabilityGate: func(context.Context, *runtime.StartRequest, contract.AllocationRuntimeHandler, string) error {
			return nil
		},
	})
	retentionTTL, err := time.ParseDuration(config.DefaultIdleRuntimeRetentionTTL)
	if err != nil {
		t.Fatalf("ParseDuration() error = %v", err)
	}
	lrtManager.ConfigureRetention(retentionTTL, config.DefaultIdleRuntimeRetentionMax)
	return testAllocationController{controller: controller, manager: manager, lrtManager: lrtManager}
}
