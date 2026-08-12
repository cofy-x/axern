package runtime

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/cgroupflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/preparedflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/rootfsflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (r *RuncServiceHandler) CreateContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	if err := validateRuncCreateRequest(request); err != nil {
		return nil, err
	}
	cgroupStart := time.Now()
	effectiveRequest, preparedOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	options = preparedOptions
	options.EphemeralStorageLimitBytes = effectiveRequest.GetEphemeralStorageLimitBytes()
	if err := r.writableCapacity.Reserve(options.ContainerID, r.Name(), effectiveRequest.GetEphemeralStorageRequestBytes(), effectiveRequest.GetEphemeralStorageLimitBytes()); err != nil {
		return nil, err
	}
	bundlePath, metaData, err := bundleflow.PrepareLaunchBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, options)
	if err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if _, err := rootfsflow.PrepareBundle(ctx, r.rootfsViews, options, bundlePath, rootfsflow.RuntimePolicy{
		RuntimeName: r.Name(), NeedsHostWritableRootfs: !effectiveRequest.GetRootfs().GetReadonly(),
		EphemeralStorageLimitBytes: effectiveRequest.GetEphemeralStorageLimitBytes(), ProjectID: r.writableCapacity.ProjectID(options.ContainerID),
		ImmutableMount: rootfsview.ImmutableMountFromProto(effectiveRequest.GetRootfs().GetImmutableMount()),
	}); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return metaData, err
	}
	if err := writeRuntimeEnforcementManifest(bundlePath, r.Name(), r.filestoreDir, effectiveRequest, options, "", r.writableCapacity.ProjectID(options.ContainerID)); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return metaData, err
	}

	return r.launchRun(ctx, options, bundlePath, metaData)
}

func (r *RuncServiceHandler) PrepareContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.PreparedContainer, error) {
	if err := validateRuncCreateRequest(request); err != nil {
		return nil, err
	}
	cgroupStart := time.Now()
	effectiveRequest, preparedOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	options = preparedOptions
	options.EphemeralStorageLimitBytes = effectiveRequest.GetEphemeralStorageLimitBytes()
	if err := r.writableCapacity.Reserve(options.ContainerID, r.Name(), effectiveRequest.GetEphemeralStorageRequestBytes(), effectiveRequest.GetEphemeralStorageLimitBytes()); err != nil {
		return nil, err
	}
	bundlePath, metaData, err := bundleflow.PrepareLaunchBundle(r.common.Loader(), r.common.ContainerRoot(), r.Name(), effectiveRequest, options)
	if err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if err := writeRuntimeEnforcementManifest(bundlePath, r.Name(), r.filestoreDir, effectiveRequest, options, "", r.writableCapacity.ProjectID(options.ContainerID)); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if _, err := rootfsflow.PrepareBundle(ctx, r.rootfsViews, options, bundlePath, rootfsflow.RuntimePolicy{
		RuntimeName: r.Name(), NeedsHostWritableRootfs: !effectiveRequest.GetRootfs().GetReadonly(),
		EphemeralStorageLimitBytes: effectiveRequest.GetEphemeralStorageLimitBytes(), ProjectID: r.writableCapacity.ProjectID(options.ContainerID),
		ImmutableMount: rootfsview.ImmutableMountFromProto(effectiveRequest.GetRootfs().GetImmutableMount()),
	}); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}

	if err := r.createPreparedContainer(ctx, metaData.Stdout, metaData.Stderr, bundlePath, options.ContainerID); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, fmt.Sprintf("create prepared container failed: %v", err))
		return nil, err
	}

	return &contract.PreparedContainer{ContainerID: options.ContainerID, BundlePath: bundlePath, Metadata: metaData}, nil
}

func (r *RuncServiceHandler) StartPreparedContainer(ctx context.Context, prepared *contract.PreparedContainer, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return preparedflow.Start(
		ctx,
		prepared,
		options,
		func(ctx context.Context, containerID string) error {
			_, err := r.common.Run(ctx, "start", containerID)
			return err
		},
		nil,
		r.waitForPreparedContainerStart,
		func(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			return runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, meta, r.waitForSandboxReady, r.readExitState)
		},
		func(ctx context.Context) error { return r.verifyMemoryEnforcement(ctx, options) },
	)
}

func validateRuncCreateRequest(request *apipb.CreateContainerRequest) error {
	if request == nil {
		return fmt.Errorf("create container request is required")
	}
	if request.CkptDir != "" {
		return fmt.Errorf("runc runtime does not support restore")
	}
	return nil
}

func (r *RuncServiceHandler) prepareCreateRequest(request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.CreateContainerRequest, contract.HandlerOptions, error) {
	request, err := resolveEphemeralStorage(request, r.ephemeralStorageDefaultLimitBytes)
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
