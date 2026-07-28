package runtime

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/launchflow"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) launchRestore(
	ctx context.Context,
	request *apipb.CreateContainerRequest,
	options contract.HandlerOptions,
	bundlePath string,
	metaData *apipb.ContainerMetadata,
) (*apipb.ContainerMetadata, error) {
	launchStart := time.Now()
	restoreStart := time.Now()
	_, err := r.runLifecycle(ctx, "restore", "--bundle", bundlePath, "--image-path", request.CkptDir, options.ContainerID)
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepRuntimeRestore, time.Since(restoreStart))
	if err != nil {
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		r.cleanupContainer(ctx, options.TraceID, options.ContainerID, fmt.Sprintf("restore failed: %v", err))
		return metaData, err
	}
	readyStart := time.Now()
	if err := runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, metaData, r.waitForSandboxReady, r.readExitState); err != nil {
		options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
		options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
		r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, fmt.Sprintf("sandboxd ready failed: %v", err))
		return metaData, err
	}
	options.RecordStartupStep(contract.StartupPhaseRuntimeLaunch, contract.StartupStepSandboxdWaitReady, time.Since(readyStart))
	options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, time.Since(launchStart))
	return metaData, nil
}

func (r *RunscServiceHandler) launchRun(
	ctx context.Context,
	options contract.HandlerOptions,
	bundlePath string,
	metaData *apipb.ContainerMetadata,
	overlayArgs []string,
) (*apipb.ContainerMetadata, error) {
	return launchflow.Run(ctx, launchflow.Options{
		HandlerOptions: options,
		BundlePath:     bundlePath,
		Metadata:       metaData,
		Start: func() (<-chan error, error) {
			return r.startRunWithExitState(metaData.Stdout, metaData.Stderr, bundlePath, options.ContainerID, overlayArgs)
		},
		WaitStart: r.waitForContainerStart,
		AfterStart: func() error {
			return r.startExitStatePersister(options.ContainerID)
		},
		OnAfterStart: func(err error) {
			logrus.WithError(err).Warnf("start runsc exit-state persister failed for %s", options.ContainerID)
		},
		WaitReady: func(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			return runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, meta, r.waitForSandboxReady, r.readExitState)
		},
		Cleanup: func(reason string) {
			r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, reason)
		},
	})
}
