package runtime

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/cgroupflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/envelopeflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/rootfsflow"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (r *RuncServiceHandler) CreateContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	cgroupStart := time.Now()
	effectiveRequest, bundleOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	if err := r.writableCapacity.Reserve(options.ContainerID, r.Name(), effectiveRequest.GetWritableLayerRequestBytes(), effectiveRequest.GetWritableLayerLimitBytes()); err != nil {
		return nil, err
	}
	if request.CkptDir != "" {
		return nil, fmt.Errorf("sample runc runtime does not support restore")
	}
	bundlePath, metaData, err := bundleflow.PrepareLaunchBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, bundleOptions)
	if err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if _, err := rootfsflow.PrepareBundle(ctx, r.rootfsViews, options, bundlePath, rootfsflow.RuntimePolicy{
		RuntimeName: r.Name(), NeedsHostWritableRootfs: !effectiveRequest.GetRootfs().GetReadonly(),
		WritableLayerLimitBytes: effectiveRequest.GetWritableLayerLimitBytes(), ProjectID: r.writableCapacity.ProjectID(options.ContainerID),
		RootfsLeaseID: effectiveRequest.GetRootfs().GetLeaseId(),
	}); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return metaData, err
	}

	return r.launchRun(ctx, options, bundlePath, metaData)
}

func (r *RuncServiceHandler) PrepareExecutionEnvelope(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.ExecutionEnvelope, error) {
	cgroupStart := time.Now()
	effectiveRequest, bundleOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	if err := r.writableCapacity.Reserve(options.ContainerID, r.Name(), effectiveRequest.GetWritableLayerRequestBytes(), effectiveRequest.GetWritableLayerLimitBytes()); err != nil {
		return nil, err
	}
	bundlePath, metaData, err := bundleflow.PrepareEnvelopeBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, bundleOptions)
	if err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if _, err := rootfsflow.PrepareBundle(ctx, r.rootfsViews, options, bundlePath, rootfsflow.RuntimePolicy{
		RuntimeName: r.Name(), NeedsHostWritableRootfs: !effectiveRequest.GetRootfs().GetReadonly(),
		WritableLayerLimitBytes: effectiveRequest.GetWritableLayerLimitBytes(), ProjectID: r.writableCapacity.ProjectID(options.ContainerID),
		RootfsLeaseID: effectiveRequest.GetRootfs().GetLeaseId(),
	}); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}

	if err := r.createExecutionEnvelope(ctx, bundlePath, options.ContainerID); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, fmt.Sprintf("create execution envelope failed: %v", err))
		return nil, err
	}

	return &contract.ExecutionEnvelope{ContainerID: options.ContainerID, BundlePath: bundlePath, Metadata: metaData}, nil
}

func (r *RuncServiceHandler) ActivateExecutionEnvelope(ctx context.Context, envelope *contract.ExecutionEnvelope, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return envelopeflow.Activate(
		ctx,
		envelope,
		options,
		func(ctx context.Context, containerID string) error {
			_, err := r.common.Run(ctx, "start", containerID)
			return err
		},
		r.startExitStatePersister,
		r.waitForEnvelopeStart,
		func(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			return runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, meta, r.waitForSandboxReady, r.readExitState)
		},
	)
}

func (r *RuncServiceHandler) prepareCreateRequest(request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.CreateContainerRequest, contract.HandlerOptions, error) {
	request, err := resolveWritableLayer(request, r.writableLayerLimitBytes)
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	prep, err := cgroupflow.PrepareRuntime(request, options, cgroupflow.RuntimePolicy{
		IgnoreCgroups:           r.ignoreCgroups,
		DropResourceWhenIgnored: true,
	})
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	return prep.Request, prep.Options, nil
}
