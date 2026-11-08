package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

const DefaultHttpSockPath = "/var/run/imagemgr.sock"

// HttpWorkerConfig holds the configuration for creating an HttpWorker.
type HttpWorkerConfig struct {
	LifecycleContext   context.Context
	OCIManager         *oci.Manager
	MountStore         *mountstore.Store
	Registry           *imageregistry.Client
	SnapshotRepository string
}

func NewHttpWorker(cfg *HttpWorkerConfig) (*HttpWorker, error) {
	if cfg.MountStore != nil {
		if err := cfg.MountStore.ValidateOCIOnly(); err != nil {
			return nil, fmt.Errorf("retired or invalid image authority: %w", err)
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
		ociMgr:             cfg.OCIManager,
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
	ociMgr             *oci.Manager
	mountStore         *mountstore.Store
	registry           *imageregistry.Client
	snapshotRepository string
	snapshotStagingDir string

	mountLocksMu sync.Mutex
	mountLocks   map[string]*mountLock
}

type mountLock struct {
	mu   sync.Mutex
	refs int
}
