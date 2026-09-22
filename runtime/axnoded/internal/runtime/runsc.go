package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

var _ contract.SandboxRuntime = &RunscServiceHandler{}
var _ contract.AllocationCapabilityVerifier = &RunscServiceHandler{}
var _ contract.RootfsSnapshotter = &RunscServiceHandler{}

type RunscServiceHandler struct {
	common                            *ocihost.Common
	ignoreCgroups                     bool
	allowSUID                         bool
	filestoreDir                      string
	ephemeralStorageDefaultLimitBytes int64
	writableCapacity                  *writableCapacityManager
	containerRoot                     string
	rootfsViews                       rootfsview.Provider
	releaseFilestore                  func()
	shutdownOnce                      sync.Once
	waitLocks                         sync.Map
	waitForSandboxReady               sandboxd.ReadyWaiter
	newWorkloadClient                 func(string) sandboxdWorkloadClient
	services                          runtimeServices
}

type runscState struct {
	Status string `json:"status"`
	Pid    int    `json:"pid"`
}

var (
	runscForceStopTimeout = 5 * time.Second
)

func (r *RunscServiceHandler) ConfigurationDigest() (string, error) {
	loaderDigest, err := r.common.Loader().ConfigurationDigest()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal([]any{loaderDigest, r.ignoreCgroups, r.allowSUID, r.ephemeralStorageDefaultLimitBytes})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r *RunscServiceHandler) FileService() contract.FileService {
	return r.services.file
}

func (r *RunscServiceHandler) lifecycleArgs(args ...string) []string {
	base := make([]string, 0, len(args)+2)
	if r.ignoreCgroups {
		base = append(base, "--ignore-cgroups")
	}
	if r.allowSUID {
		base = append(base, "--allow-suid")
	}
	base = append(base, args...)
	return base
}

func (r *RunscServiceHandler) runLifecycle(ctx context.Context, args ...string) ([]byte, error) {
	return r.common.Run(ctx, r.lifecycleArgs(args...)...)
}

func (r *RunscServiceHandler) Version(ctx context.Context) (*runtimeapi.RuntimeVersion, error) {
	version, err := r.common.Version(ctx)
	if err != nil {
		return nil, err
	}
	return &runtimeapi.RuntimeVersion{
		Version: version,
	}, nil
}

func (r *RunscServiceHandler) ShutDown() {
	r.shutdownOnce.Do(r.releaseFilestore)
}
