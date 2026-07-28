package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/cgroupflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/envelopeflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/rootfsflow"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) CreateContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
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
	overlayArgsStart := time.Now()
	overlayArgs, err := r.overlayArgsForBundle(bundlePath)
	if err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	overlayArgs = runscSandboxdArgs(overlayArgs)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))

	if request.CkptDir != "" {
		return r.launchRestore(ctx, request, options, bundlePath, metaData)
	}
	return r.launchRun(ctx, options, bundlePath, metaData, overlayArgs)
}

func (r *RunscServiceHandler) PrepareExecutionEnvelope(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.ExecutionEnvelope, error) {
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
	overlayArgsStart := time.Now()
	overlayArgs, err := r.overlayArgsForBundle(bundlePath)
	if err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	overlayArgs = runscSandboxdArgs(overlayArgs)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))

	if err := r.createExecutionEnvelope(ctx, bundlePath, options.ContainerID, overlayArgs); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, fmt.Sprintf("create execution envelope failed: %v", err))
		return nil, err
	}

	return &contract.ExecutionEnvelope{ContainerID: options.ContainerID, BundlePath: bundlePath, Metadata: metaData}, nil
}

func (r *RunscServiceHandler) ActivateExecutionEnvelope(ctx context.Context, envelope *contract.ExecutionEnvelope, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return envelopeflow.Activate(
		ctx,
		envelope,
		options,
		func(ctx context.Context, containerID string) error {
			_, err := r.runLifecycle(ctx, "start", containerID)
			return err
		},
		r.startExitStatePersister,
		r.waitForEnvelopeStart,
		func(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			return runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, meta, r.waitForSandboxReady, r.readExitState)
		},
	)
}

func (r *RunscServiceHandler) prepareCreateRequest(request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.CreateContainerRequest, contract.HandlerOptions, error) {
	prep, err := cgroupflow.PrepareRuntime(request, options, cgroupflow.RuntimePolicy{
		IgnoreCgroups:                r.ignoreCgroups,
		AllowWritePermissionFallback: true,
	})
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	if prep.WritePermissionFallback {
		logrus.Warnf("cgroup controller is not writable for %s, continuing without runtime resource limits: %v", prep.Options.RuntimeCgroupPath, prep.WritePermissionError)
	}
	return prep.Request, prep.Options, nil
}
