package resources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"google.golang.org/protobuf/proto"
)

type stateStore interface {
	SaveSnapshot(bucket string, value proto.Message) error
	LoadSnapshot(bucket string, value proto.Message) error
}

type Manager interface {
	Allocate(opt AllocateOption) (Resource, error)
	// Recycle releases a resource and must be idempotent so a partially
	// successful multi-resource cleanup can be retried safely.
	Recycle(id string) error
	// Status returns the using and idle resource ids.
	Status() ([]string, []string)
	ShutDown() error
	ResourceName() ResourceName
}

type AllocateOption struct {
	Context            context.Context
	ContainerID        string
	EnvID              string
	FunctionName       string
	TraceID            string
	MemoryRequestBytes int64
	MemoryLimitBytes   int64
	AllocationAttempt  int64
	RuntimeName        string
	CgroupOwnerKind    apipb.CgroupLeaseOwnerKind
}

// RetiringMemoryLease is the durable information needed to keep reporting an
// allocation's memcg after its runtime and bundle state have been removed.
// Kernel identity and usage are sampled from the live cgroup, never trusted
// from this record.
type RetiringMemoryLease struct {
	CgroupID          string
	AllocationID      string
	AllocationAttempt int64
	MemoryRequest     int64
	MemoryLimit       int64
	RuntimeName       string
	BootID            string
	MountIdentity     string
	ParentInode       uint64
	LeafInode         uint64
}

// resizable extends Manager with pool sizing methods used by the internal resize loop.
type resizable interface {
	Manager
	Add(num int) int
	Del(num int)
	CacheNum() int
	UsingNum() int
	MaxSizeLimit() int
	CacheSizeLimit() int
}

// NewResourceManager will init all resource manager and start to sync resource.
func NewResourceManager(db stateStore, cfg config.Config) ([]Manager, error) {
	managers := make([]Manager, 0)
	var resizables []resizable
	cleanup := func(initErr error) error {
		var cleanupErrs []error
		for i := len(managers) - 1; i >= 0; i-- {
			if err := managers[i].ShutDown(); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("shut down %s manager: %w", managers[i].ResourceName(), err))
			}
		}
		return errors.Join(append([]error{initErr}, cleanupErrs...)...)
	}
	reconcileInterval, err := cfg.PluginConfig.ResourceConfig.ResourcePoolReconcileIntervalDuration()
	if err != nil {
		return nil, err
	}
	cgroupMode, err := cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return nil, err
	}

	if cfg.InterfaceCacheSize > 0 {
		interfaceManager, err := LoadNetworkManager(db, cfg.MaxInstanceNum, cfg.InterfaceCacheSize, cfg.PluginConfig.NetworkConfig)
		if err != nil {
			return nil, err
		}
		managers = append(managers, interfaceManager)
		if r, ok := interfaceManager.(resizable); ok {
			resizables = append(resizables, r)
			metrics.RecordResourceGauge("interface", float64(r.CacheSizeLimit()))
		}
	}

	// Enforcement enablement and warm-pool sizing are independent contracts.
	// A required node always owns a cgroup manager; cache_size=0 merely asks it
	// to create each one-use allocation cgroup synchronously.
	if cgroupMode == config.CgroupEnforcementRequired {
		cgroupManager, err := NewCgroupManager(db, cfg.ResourceConfig, cgroupMode == config.CgroupEnforcementRequired)
		if err != nil {
			return nil, cleanup(err)
		}
		managers = append(managers, cgroupManager)
		resizables = append(resizables, cgroupManager)
		metrics.RecordResourceGauge("cgroup", float64(cgroupManager.CacheSizeLimit()))
	}

	runManager(reconcileInterval, resizables...)
	return managers, nil
}
