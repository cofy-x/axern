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
	if request.CkptDir != "" {
		return nil, fmt.Errorf("sample runc runtime does not support restore")
	}
	effectiveRequest, writableRootfs, err := rootfsflow.PrepareRequest(ctx, r.rootfsViews, options, effectiveRequest)
	if err != nil {
		return nil, err
	}

	bundlePath, metaData, err := bundleflow.PrepareLaunchBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, bundleOptions)
	if err != nil {
		if writableRootfs {
			r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		}
		return nil, err
	}
	mountTargetsStart := time.Now()
	if err := bundleflow.PrepareBundleMountTargets(bundlePath); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepMountTargetsPrepare, time.Since(mountTargetsStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return metaData, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepMountTargetsPrepare, time.Since(mountTargetsStart))

	return r.launchRun(ctx, options, bundlePath, metaData)
}

func (r *RuncServiceHandler) PrepareExecutionEnvelope(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.ExecutionEnvelope, error) {
	cgroupStart := time.Now()
	effectiveRequest, bundleOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	effectiveRequest, writableRootfs, err := rootfsflow.PrepareRequest(ctx, r.rootfsViews, options, effectiveRequest)
	if err != nil {
		return nil, err
	}
	bundlePath, metaData, err := bundleflow.PrepareEnvelopeBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, bundleOptions)
	if err != nil {
		if writableRootfs {
			r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		}
		return nil, err
	}
	mountTargetsStart := time.Now()
	if err := bundleflow.PrepareBundleMountTargets(bundlePath); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepMountTargetsPrepare, time.Since(mountTargetsStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepMountTargetsPrepare, time.Since(mountTargetsStart))

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
	prep, err := cgroupflow.PrepareRuntime(request, options, cgroupflow.RuntimePolicy{
		IgnoreCgroups:           r.ignoreCgroups,
		DropResourceWhenIgnored: true,
	})
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	return prep.Request, prep.Options, nil
}
