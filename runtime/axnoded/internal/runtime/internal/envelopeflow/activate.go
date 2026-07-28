package envelopeflow

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func Activate(
	ctx context.Context,
	envelope *contract.ExecutionEnvelope,
	options contract.HandlerOptions,
	start func(context.Context, string) error,
	startExitStatePersister func(string) error,
	waitForStart func(context.Context, string) error,
	waitForSandboxReady func(context.Context, string, *apipb.ContainerMetadata) error,
) (*apipb.ContainerMetadata, error) {
	if envelope == nil {
		return nil, fmt.Errorf("execution envelope is nil")
	}

	launchStart := time.Now()
	if err := start(ctx, envelope.ContainerID); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeStart, time.Since(launchStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		return envelope.Metadata, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeStart, time.Since(launchStart))
	exitMonitorStart := time.Now()
	if err := startExitStatePersister(envelope.ContainerID); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeExitMonitor, time.Since(exitMonitorStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		return envelope.Metadata, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeExitMonitor, time.Since(exitMonitorStart))
	waitStart := time.Now()
	if err := waitForStart(ctx, envelope.ContainerID); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		return envelope.Metadata, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
	if waitForSandboxReady != nil {
		readyStart := time.Now()
		if err := waitForSandboxReady(ctx, envelope.BundlePath, envelope.Metadata); err != nil {
			options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
			options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
			return envelope.Metadata, err
		}
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
	}
	options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
	return envelope.Metadata, nil
}
