package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/cgroupflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/rootfsflow"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (r *RunscServiceHandler) PrepareContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.PreparedContainer, error) {
	cgroupStart := time.Now()
	effectiveRequest, preparedOptions, err := r.prepareCreateRequest(request, options)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeCgroupPrepare, time.Since(cgroupStart))
	if err != nil {
		return nil, err
	}
	options = preparedOptions
	options.EphemeralStorageLimitBytes = effectiveRequest.GetEphemeralStorageLimitBytes()
	if err := r.writableCapacity.Charge(options.ContainerID, config.RuntimeNameRunsc, effectiveRequest.GetEphemeralStorageRequestBytes(), effectiveRequest.GetEphemeralStorageLimitBytes()); err != nil {
		return nil, err
	}
	bundlePath, metaData, err := bundleflow.PrepareLaunchBundle(r.common.Loader(), r.common.ContainerRoot(), config.RuntimeNameRunsc, effectiveRequest, options)
	if err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	if _, err := rootfsflow.PrepareBundle(ctx, r.rootfsViews, options, bundlePath, rootfsview.ImmutableMountFromProto(effectiveRequest.GetRootfs().GetImmutableMount())); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	overlayArgsStart := time.Now()
	overlayArgs, err := r.overlayArgsForBundle(bundlePath, effectiveRequest.GetEphemeralStorageLimitBytes())
	if err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}
	overlayValue := ""
	if len(overlayArgs) > 0 {
		overlayValue = overlayArgs[len(overlayArgs)-1]
	}
	overlayArgs = runscSandboxdArgs(overlayArgs)
	options.RecordStartupStep(contract.StartupPhaseRuntimeBundle, contract.StartupStepRuntimeOverlayArgs, time.Since(overlayArgsStart))
	if err := writeRuntimeEnforcementManifest(bundlePath, config.RuntimeNameRunsc, r.filestoreDir, effectiveRequest, options, overlayValue); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, err.Error())
		return nil, err
	}

	if err := r.createPreparedContainer(ctx, metaData.Stdout, metaData.Stderr, bundlePath, options.ContainerID, overlayArgs, effectiveRequest.GetResource(), options); err != nil {
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, fmt.Sprintf("create prepared container failed: %v", err))
		return nil, err
	}

	return &contract.PreparedContainer{ContainerID: options.ContainerID, BundlePath: bundlePath, Metadata: metaData}, nil
}

func (r *RunscServiceHandler) StartPreparedContainer(ctx context.Context, prepared *contract.PreparedContainer, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	if prepared == nil {
		return nil, fmt.Errorf("prepared container is nil")
	}
	launchStart := time.Now()
	defer func() {
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
	}()
	stepStart := launchStart
	recordStep := func(step contract.StartupStep) {
		now := time.Now()
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, step, now.Sub(stepStart))
		stepStart = now
	}

	id := prepared.ContainerID
	err := r.startPreparedRuntime(ctx, id, options)
	recordStep(contract.StartupStepRuntimeStart)
	if err != nil {
		return prepared.Metadata, err
	}
	err = r.startExitStatePersister(id)
	recordStep(contract.StartupStepRuntimeExitMonitor)
	if err != nil {
		return prepared.Metadata, err
	}
	err = r.waitForPreparedContainerStart(ctx, id)
	recordStep(contract.StartupStepRuntimeWaitStart)
	if err != nil {
		return prepared.Metadata, err
	}
	if r.guestTaskLimit > 0 {
		err = r.configureGuestTaskLimits(ctx, id, options.EnforcementManifest)
	}
	recordStep(contract.StartupStepRuntimeEnforcement)
	if err != nil {
		return prepared.Metadata, err
	}
	err = runtimesandboxd.WaitReadyOrExit(ctx, config.RuntimeNameRunsc, options.ContainerID, prepared.BundlePath, prepared.Metadata, r.waitForSandboxReady, r.readExitState)
	recordStep(contract.StartupStepSandboxdWaitReady)
	if err != nil {
		return prepared.Metadata, err
	}
	err = r.verifyMemoryEnforcement(ctx, options)
	recordStep(contract.StartupStepRuntimeEnforcement)
	return prepared.Metadata, err
}

func (r *RunscServiceHandler) startPreparedRuntime(ctx context.Context, id string, options contract.HandlerOptions) error {
	args, err := r.preparedContainerLifecycleArgs(options, id, "start", id)
	if err != nil {
		return err
	}
	if r.guestTaskLimit > 0 {
		if err := r.validateTaskEnforcementManifest(ctx, id, options.EnforcementManifest); err != nil {
			return err
		}
	}
	if r.nativeRecovery {
		return r.startWithTransitionFence(ctx, id, args)
	}
	_, err = r.common.Run(ctx, args...)
	return err
}

func (r *RunscServiceHandler) prepareCreateRequest(request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.CreateContainerRequest, contract.HandlerOptions, error) {
	request, err := resolveEphemeralStorage(request, r.ephemeralStorageDefaultLimitBytes)
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	prep, err := cgroupflow.PrepareRuntime(request, options, cgroupflow.RuntimePolicy{
		IgnoreCgroups: r.ignoreCgroups,
	})
	if err != nil {
		return nil, contract.HandlerOptions{}, err
	}
	if r.hostTaskLimit > 0 {
		if err := configureTaskPIDDomain(prep.Options.CgroupPath, prep.Options.RuntimeCgroupPath, r.hostTaskLimit); err != nil {
			return nil, contract.HandlerOptions{}, fmt.Errorf("configure host task domain: %w", err)
		}
	}
	prep.Options = r.withTaskExecutionProfiles(prep.Options)
	return prep.Request, prep.Options, nil
}

func (r *RunscServiceHandler) withTaskExecutionProfiles(options contract.HandlerOptions) contract.HandlerOptions {
	options.ExecutionProfile = r.withTaskExecutionProfile(options.ExecutionProfile)
	if options.BundleTemplateSource != nil {
		source := *options.BundleTemplateSource
		if source.ExecutionProfile == nil {
			source.ExecutionProfile = options.ExecutionProfile
		} else {
			source.ExecutionProfile = r.withTaskExecutionProfile(source.ExecutionProfile)
		}
		options.BundleTemplateSource = &source
	}
	return options
}

func (r *RunscServiceHandler) withTaskExecutionProfile(original *runtimeoci.ExecutionProfile) *runtimeoci.ExecutionProfile {
	if r.guestTaskLimit == 0 && original == nil {
		return nil
	}
	profile := runtimeoci.DefaultExecutionProfile()
	if original != nil {
		profile = *original
	}
	profile.GuestTaskLimit = r.guestTaskLimit
	profile.HostTaskLimit = r.hostTaskLimit
	return &profile
}
