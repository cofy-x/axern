package preparedflow

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func Start(
	ctx context.Context,
	prepared *contract.PreparedContainer,
	options contract.HandlerOptions,
	start func(context.Context, string) error,
	startExitStatePersister func(string) error,
	waitForStart func(context.Context, string) error,
	waitForSandboxReady func(context.Context, string, *apipb.ContainerMetadata) error,
	verifyRuntime func(context.Context) error,
) (*apipb.ContainerMetadata, error) {
	if prepared == nil {
		return nil, fmt.Errorf("prepared container is nil")
	}
	launchStart := time.Now()
	if err := start(ctx, prepared.ContainerID); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeStart, time.Since(launchStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		return prepared.Metadata, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeStart, time.Since(launchStart))
	exitMonitorStart := time.Now()
	if startExitStatePersister != nil {
		if err := startExitStatePersister(prepared.ContainerID); err != nil {
			options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeExitMonitor, time.Since(exitMonitorStart))
			options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
			return prepared.Metadata, err
		}
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeExitMonitor, time.Since(exitMonitorStart))
	waitStart := time.Now()
	if err := waitForStart(ctx, prepared.ContainerID); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		return prepared.Metadata, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
	if waitForSandboxReady != nil {
		readyStart := time.Now()
		if err := waitForSandboxReady(ctx, prepared.BundlePath, prepared.Metadata); err != nil {
			options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
			options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
			return prepared.Metadata, err
		}
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
	}
	if verifyRuntime != nil {
		verifyStart := time.Now()
		if err := verifyRuntime(ctx); err != nil {
			options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeEnforcement, time.Since(verifyStart))
			options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
			return prepared.Metadata, err
		}
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeEnforcement, time.Since(verifyStart))
	}
	options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
	return prepared.Metadata, nil
}
