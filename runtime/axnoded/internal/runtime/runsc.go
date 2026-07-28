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

var _ contract.RuntimeHandler = &RunscServiceHandler{}

type RunscServiceHandler struct {
	name                string
	common              *ocihost.Common
	ignoreCgroups       bool
	allowSUID           bool
	filestoreDir        string
	overlayTmpfsSize    string
	rootfsViews         rootfsview.Provider
	waitLocks           sync.Map
	waitForSandboxReady sandboxd.ReadyWaiter
	services            runtimeServices
}

type runscState struct {
	Status string `json:"status"`
}

var (
	runscExitStateGracePeriod = 5 * time.Second
	runscWaitRetryTimeout     = 200 * time.Millisecond
)

func (r *RunscServiceHandler) Name() string {
	return r.name
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
		RuntimeName:    r.Name(),
		RuntimeVersion: version,
	}, nil
}

func (r *RunscServiceHandler) ShutDown() {}
