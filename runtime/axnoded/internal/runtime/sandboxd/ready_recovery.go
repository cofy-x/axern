package sandboxd

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/sirupsen/logrus"
)

type ExitStateReader func(string) (contract.Exit, bool, error)

func WaitReadyOrExit(
	ctx context.Context,
	runtimeName string,
	containerID string,
	bundlePath string,
	meta *apipb.ContainerMetadata,
	waitReady ReadyWaiter,
	readExit ExitStateReader,
) error {
	if waitReady == nil {
		return fmt.Errorf("%s sandboxd ready waiter is nil", runtimeName)
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	readyCh := make(chan error, 1)
	go func() {
		readyCh <- waitReady(waitCtx, bundlePath, meta)
	}()

	ticker := time.NewTicker(DefaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-readyCh:
			return err
		case <-ticker.C:
			if ok, err := acceptExitBeforeReady(runtimeName, containerID, bundlePath, meta, readExit); ok || err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func acceptExitBeforeReady(runtimeName, containerID, bundlePath string, meta *apipb.ContainerMetadata, readExit ExitStateReader) (bool, error) {
	if readExit == nil {
		return false, nil
	}
	exit, ok, err := readExit(containerID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	markExitedBeforeReady(meta, runtimeoci.SandboxdBundleSocketPath(bundlePath))
	logrus.WithField("exit_code", exit.Status).Debugf("%s workload %s exited before sandboxd readiness was observed", runtimeName, containerID)
	return true, nil
}

func markExitedBeforeReady(meta *apipb.ContainerMetadata, socketPath string) {
	if meta == nil {
		return
	}
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[LabelReady] = "false"
	meta.Labels[LabelSocket] = socketPath
	meta.Labels[LabelCapabilities] = ""
	meta.Labels[LabelUserState] = "exited"
}
