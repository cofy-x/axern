package launchflow

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type Options struct {
	HandlerOptions contract.HandlerOptions
	BundlePath     string
	Metadata       *apipb.ContainerMetadata
	Start          func() (<-chan error, error)
	WaitStart      func(context.Context, string, <-chan error) error
	AfterStart     func() error
	OnAfterStart   func(error)
	WaitReady      func(context.Context, string, *apipb.ContainerMetadata) error
	Cleanup        func(string)
}

func Run(ctx context.Context, options Options) (*apipb.ContainerMetadata, error) {
	launchStart := time.Now()
	fail := func(reason string, err error) (*apipb.ContainerMetadata, error) {
		options.HandlerOptions.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		if options.Cleanup != nil {
			options.Cleanup(fmt.Sprintf("%s: %v", reason, err))
		}
		return options.Metadata, err
	}

	runWait, err := options.Start()
	options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeStart, time.Since(launchStart))
	if err != nil {
		return fail("run failed", err)
	}
	waitStart := time.Now()
	if err := options.WaitStart(ctx, options.HandlerOptions.ContainerID, runWait); err != nil {
		options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
		return fail("run startup failed", err)
	}
	options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeWaitStart, time.Since(waitStart))
	if options.AfterStart != nil {
		afterStart := time.Now()
		if err := options.AfterStart(); err != nil && options.OnAfterStart != nil {
			options.OnAfterStart(err)
		}
		options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeExitMonitor, time.Since(afterStart))
	}
	readyStart := time.Now()
	if err := options.WaitReady(ctx, options.BundlePath, options.Metadata); err != nil {
		options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
		return fail("sandboxd ready failed", err)
	}
	options.HandlerOptions.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
	options.HandlerOptions.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
	return options.Metadata, nil
}
