package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
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
	LifecycleContext   context.Context
	Manager            imagefsd.Manager
	OCIManager         *oci.Manager
	NydusClient        *nydus.RegistryClient
	NydusSuffix        string
	RegistryProxyURL   string
	MountStore         *mountstore.Store
	Registry           *imageregistry.Client
	SnapshotRepository string
}

func NewHttpWorker(cfg *HttpWorkerConfig) (*HttpWorker, error) {
	if cfg.MountStore != nil {
		records, err := cfg.MountStore.List()
		if err != nil {
			return nil, fmt.Errorf("read persisted mount records: %w", err)
		}
		for _, record := range records {
			if MountType(record.MountType) != MountTypeOCI && MountType(record.MountType) != MountTypeNydus {
				return nil, fmt.Errorf("invalid persisted mount type %q", record.MountType)
			}
		}
	}
	lifecycleCtx := cfg.LifecycleContext
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}
	snapshotRepository := strings.TrimSpace(cfg.SnapshotRepository)
	snapshotStagingDir := ""
	if snapshotRepository != "" {
		if cfg.OCIManager == nil || cfg.Registry == nil {
			return nil, fmt.Errorf("snapshot repository requires OCI manager and registry client")
		}
		var err error
		snapshotStagingDir, err = prepareRootfsSnapshotStaging(cfg.OCIManager.Root())
		if err != nil {
			return nil, err
		}
	}
	w := &HttpWorker{
		lifecycleCtx:       lifecycleCtx,
		mgr:                cfg.Manager,
		ociMgr:             cfg.OCIManager,
		nydusClient:        cfg.NydusClient,
		nydusSuffix:        cfg.NydusSuffix,
		registryProxyURL:   cfg.RegistryProxyURL,
		nydusCache:         newNydusImageCache(),
		mountStore:         cfg.MountStore,
		registry:           cfg.Registry,
		snapshotRepository: snapshotRepository,
		snapshotStagingDir: snapshotStagingDir,
		mountLocks:         make(map[string]*mountLock),
	}
	if w.mountStore != nil {
		go w.runMountReleaseReconciler(lifecycleCtx)
	}
	return w, nil
}

// prepareRootfsSnapshotStaging removes files left by an interrupted imagemgr
// process before the new API server can accept snapshot requests. Active
// requests still own and remove their individual files through SnapshotRootfs.
func prepareRootfsSnapshotStaging(ociRoot string) (string, error) {
	ociRoot = strings.TrimSpace(ociRoot)
	if ociRoot == "" {
		return "", fmt.Errorf("OCI root is required for snapshot staging")
	}
	dir := filepath.Join(ociRoot, "snapshots", "staging")
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("remove abandoned snapshot staging: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create snapshot staging directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", fmt.Errorf("secure snapshot staging directory: %w", err)
	}
	return dir, nil
}

type HttpWorker struct {
	lifecycleCtx       context.Context
	mgr                imagefsd.Manager
	ociMgr             *oci.Manager
	nydusClient        *nydus.RegistryClient
	nydusSuffix        string
	registryProxyURL   string
	nydusCache         *nydusImageCache
	mountStore         *mountstore.Store
	registry           *imageregistry.Client
	snapshotRepository string
	snapshotStagingDir string

	nydusDetectSF singleflight.Group
	mountLocksMu  sync.Mutex
	mountLocks    map[string]*mountLock
}

type mountLock struct {
	mu   sync.Mutex
	refs int
}
