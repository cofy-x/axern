package rootfsflow

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"google.golang.org/protobuf/proto"
)

// PrepareRequest resolves a writable rootfs view before bundle materialization.
func PrepareRequest(
	ctx context.Context,
	provider rootfsview.Provider,
	options contract.HandlerOptions,
	request *apipb.CreateContainerRequest,
) (*apipb.CreateContainerRequest, bool, error) {
	prepareStart := time.Now()
	view, err := provider.Prepare(ctx, options.ContainerID, bundleflow.RootfsViewSource(request.GetRootfs()))
	options.RecordStartupStep(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsViewPrepare, time.Since(prepareStart))
	if err != nil {
		return nil, false, fmt.Errorf("prepare writable rootfs failed: %w", err)
	}
	if !view.Writable {
		return request, false, nil
	}

	applyStart := time.Now()
	effectiveRequest := proto.Clone(request).(*apipb.CreateContainerRequest)
	effectiveRequest.Rootfs.RootDir = view.RootDir
	effectiveRequest.Rootfs.Readonly = false
	options.RecordStartupStep(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsViewApply, time.Since(applyStart))
	return effectiveRequest, true, nil
}
