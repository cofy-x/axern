package allocation

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
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
	controller       *Controller
	manager          *container.Manager
	environmentCache *environmentcache.EnvironmentCache
}

func newTestAllocationController(t *testing.T, runscHandler contract.SandboxRuntime) testAllocationController {
	t.Helper()
	return newTestAllocationControllerWithStore(t, runscHandler, storetest.NewMockStore())
}

func newTestAllocationControllerWithStore(t *testing.T, runscHandler contract.SandboxRuntime, dbStore testStateStore) testAllocationController {
	return newTestAllocationControllerWithResources(t, runscHandler, dbStore, newTestResourceManagers()...)
}

func newTestAllocationControllerWithResources(t *testing.T, runscHandler contract.SandboxRuntime, dbStore testStateStore, managers ...resourcemanager.Manager) testAllocationController {
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
	if runscHandler == nil {
		runscHandler = runtimetest.NewFakeSandboxRuntime()
	}
	manager, err := container.NewManager(tmpDir, runscHandler, make(chan bool, 10), managers...)
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
	environmentCache := environmentcache.NewEnvironmentCache()
	networking := servicenetworking.NewCoordinator(servicenetworking.Options{
		NatBackend: cfg.NatBackend,
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
		RunscHandler:     runscHandler,
		EnvironmentCache: environmentCache,
		Networking:       networking,
		PreActivationCapabilityGate: func(context.Context, *runtime.StartRequest, contract.AllocationRuntime, string) error {
			return nil
		},
	})
	retentionTTL, err := time.ParseDuration(config.DefaultIdleEnvironmentRetentionTTL)
	if err != nil {
		t.Fatalf("ParseDuration() error = %v", err)
	}
	environmentCache.ConfigureRetention(retentionTTL, config.DefaultIdleEnvironmentRetentionMax)
	return testAllocationController{controller: controller, manager: manager, environmentCache: environmentCache}
}
