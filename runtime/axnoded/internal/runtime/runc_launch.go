package runtime

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/launchflow"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (r *RuncServiceHandler) launchRun(
	ctx context.Context,
	options contract.HandlerOptions,
	bundlePath string,
	metaData *apipb.ContainerMetadata,
) (*apipb.ContainerMetadata, error) {
	return launchflow.Run(ctx, launchflow.Options{
		HandlerOptions: options,
		BundlePath:     bundlePath,
		Metadata:       metaData,
		Start: func() (<-chan error, error) {
			return r.startRunWithExitState(metaData.Stdout, metaData.Stderr, bundlePath, options.ContainerID)
		},
		WaitStart: r.waitForContainerStart,
		WaitReady: func(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			if err := r.verifyMemoryEnforcement(ctx, options); err != nil {
				return err
			}
			return runtimesandboxd.WaitReadyOrExit(ctx, r.Name(), options.ContainerID, bundlePath, meta, r.waitForSandboxReady, r.readExitState)
		},
		Cleanup: func(reason string) {
			r.cleanupContainer(context.Background(), options.TraceID, options.ContainerID, reason)
		},
	})
}
