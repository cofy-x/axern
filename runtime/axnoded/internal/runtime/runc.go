package runtime

import (
	"context"
	"sync"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

var _ contract.RuntimeHandler = &RuncServiceHandler{}
var _ contract.AllocationCapabilityVerifier = &RuncServiceHandler{}

type RuncServiceHandler struct {
	name                              string
	common                            *ocihost.Common
	ignoreCgroups                     bool
	ephemeralStorageDefaultLimitBytes int64
	writableCapacity                  *writableCapacityManager
	filestoreDir                      string
	containerRoot                     string
	rootfsViews                       rootfsview.Provider
	releaseFilestore                  func()
	shutdownOnce                      sync.Once
	waitLocks                         sync.Map
	waitForSandboxReady               sandboxd.ReadyWaiter
	services                          runtimeServices
}

type runcState struct {
	Status string `json:"status"`
	Pid    *int   `json:"pid"`
}

var runcExitStateGracePeriod = 5 * time.Second
var runcDeleteMonitorWaitTimeout = 5 * time.Second

func (r *RuncServiceHandler) Name() string {
	return r.name
}

func (r *RuncServiceHandler) FileService() contract.FileService {
	return r.services.file
}

func (r *RuncServiceHandler) Version(ctx context.Context) (*runtimeapi.RuntimeVersion, error) {
	version, err := r.common.Version(ctx)
	if err != nil {
		return nil, err
	}
	return &runtimeapi.RuntimeVersion{
		RuntimeName:    r.Name(),
		RuntimeVersion: version,
	}, nil
}

func (r *RuncServiceHandler) ShutDown() {
	r.shutdownOnce.Do(r.releaseFilestore)
}
