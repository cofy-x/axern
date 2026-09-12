package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
)

const DefaultHttpSockPath = "/var/run/imagemgr.sock"

// daemonIDSchemaVersion binds daemon identities to the current imagefsd config schema.
const daemonIDSchemaVersion = "v3"

func generateNydusID(imageURL string) string {
	cs := sha256.Sum256([]byte(daemonIDSchemaVersion + ":nydus:" + imageURL))
	return hex.EncodeToString(cs[:])
}

// HttpWorkerConfig holds the configuration for creating an HttpWorker.
type HttpWorkerConfig struct {
	LifecycleContext context.Context
	Manager          imagefsd.Manager
	OCIManager       *oci.Manager
	NydusClient      *nydus.RegistryClient
	NydusSuffix      string
	RegistryProxyURL string
	MountStore       *mountstore.Store
}

func NewHttpWorker(cfg *HttpWorkerConfig) (*HttpWorker, error) {
	if cfg.MountStore != nil {
		records, err := cfg.MountStore.List()
		if err != nil {
			return nil, fmt.Errorf("read persisted mount records: %w", err)
		}
		for _, record := range records {
			if MountType(record.MountType) != MountTypeOCI && MountType(record.MountType) != MountTypeNydus {
				return nil, fmt.Errorf("unsupported persisted mount type %q; resolve legacy mounts before starting imagemgr", record.MountType)
			}
		}
	}
	lifecycleCtx := cfg.LifecycleContext
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}
	w := &HttpWorker{
		lifecycleCtx:     lifecycleCtx,
		mgr:              cfg.Manager,
		ociMgr:           cfg.OCIManager,
		nydusClient:      cfg.NydusClient,
		nydusSuffix:      cfg.NydusSuffix,
		registryProxyURL: cfg.RegistryProxyURL,
		nydusCache:       newNydusImageCache(),
		mountStore:       cfg.MountStore,
		mountLocks:       make(map[string]*mountLock),
	}
	if w.mountStore != nil {
		go w.runMountReleaseReconciler(lifecycleCtx)
	}
	return w, nil
}

type HttpWorker struct {
	lifecycleCtx     context.Context
	mgr              imagefsd.Manager
	ociMgr           *oci.Manager
	nydusClient      *nydus.RegistryClient
	nydusSuffix      string
	registryProxyURL string
	nydusCache       *nydusImageCache
	mountStore       *mountstore.Store

	nydusDetectSF singleflight.Group
	mountLocksMu  sync.Mutex
	mountLocks    map[string]*mountLock
}

type mountLock struct {
	mu   sync.Mutex
	refs int
}
