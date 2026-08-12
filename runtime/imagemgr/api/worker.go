package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/ossloop"
)

const DefaultHttpSockPath = "/var/run/imagemgr.sock"

// daemonIDSchemaVersion binds daemon identities to the current imagefsd config schema.
const daemonIDSchemaVersion = "v3"

type OSSLoopManager interface {
	EnsureMounted(id, imagePath string) (string, error)
	EffectiveLowerDirs(id string) ([]string, error)
	ReleaseResource(id string) (ossloop.UnmountResult, error)
}

func generateOSSID(endpoint, bucket, object string) string {
	cs := sha256.Sum256([]byte(daemonIDSchemaVersion + ":" + endpoint + bucket + object))
	return hex.EncodeToString(cs[:])
}

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
	OSSLoopManager   OSSLoopManager
	MountStore       *mountstore.Store
}

func NewHttpWorker(cfg *HttpWorkerConfig) (*HttpWorker, error) {
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
		ossLoopMgr:       cfg.OSSLoopManager,
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
	ossLoopMgr       OSSLoopManager
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

func splitObject(object string) (string, string, error) {
	if object == "" {
		return "", "", fmt.Errorf("empty object")
	}
	if strings.HasSuffix(object, "/") {
		return "", "", fmt.Errorf("object should not end with '/'")
	}
	cleaned := path.Clean(object)
	if cleaned == "." || cleaned == "/" {
		return "", "", fmt.Errorf("invalid object: %s", object)
	}
	dir := path.Dir(cleaned)
	if dir == "." {
		dir = ""
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	return prefix, path.Base(cleaned), nil
}
