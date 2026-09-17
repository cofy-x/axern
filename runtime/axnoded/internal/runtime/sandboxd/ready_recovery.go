package sandboxd

import (
	"context"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
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
			if ok, err := rejectExitBeforeReady(runtimeName, containerID, readExit); ok || err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func rejectExitBeforeReady(runtimeName, containerID string, readExit ExitStateReader) (bool, error) {
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
	logrus.WithField("exit_code", exit.Status).Warnf("%s OCI sandbox %s exited before sandboxd readiness was observed", runtimeName, containerID)
	return true, fmt.Errorf("%s OCI sandbox %s exited with status %d before sandboxd readiness", runtimeName, containerID, exit.Status)
}
