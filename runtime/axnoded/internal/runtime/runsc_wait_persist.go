package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) waitWithOCI(ctx context.Context, containerID string) (contract.Exit, bool, error) {
	return r.common.Wait(ctx, r.lifecycleArgs("wait", containerID)...)
}

func (r *RunscServiceHandler) acceptWaitExit(ctx context.Context, containerID string, exit contract.Exit) (bool, error) {
	if state, stateErr := r.state(ctx, containerID); stateErr == nil && state.Status == string(contract.ContainerStatusRunning) {
		logrus.Debugf("runsc wait returned status %d for %s while state still reports running; ignoring transient wait result", exit.Status, containerID)
		return false, nil
	}
	return true, r.persistExitState(containerID, exit)
}

func (r *RunscServiceHandler) startExitStatePersister(containerID string) error {
	go func() {
		waitLock := r.waitLock(containerID)
		waitLock.Lock()
		defer waitLock.Unlock()

		if _, ok, err := r.readExitState(containerID); ok || err != nil {
			return
		}

		for {
			exit, ok, err := r.waitWithOCI(context.Background(), containerID)
			if err != nil {
				logrus.WithError(err).Warnf("runsc exit-state persister failed for %s", containerID)
				return
			}
			if !ok {
				return
			}

			accepted, acceptErr := r.acceptWaitExit(context.Background(), containerID, contract.Exit{
				Timestamp: exit.Timestamp.UTC(),
				Status:    exit.Status,
			})
			if acceptErr != nil {
				logrus.WithError(acceptErr).Warnf("persist runsc wait exit state failed for %s", containerID)
				return
			}
			if accepted {
				return
			}

			time.Sleep(100 * time.Millisecond)
		}
	}()
	return nil
}

func (r *RunscServiceHandler) readExitState(containerID string) (contract.Exit, bool, error) {
	return r.common.ReadExitState(containerID, "runsc")
}

func (r *RunscServiceHandler) persistExitState(containerID string, exit contract.Exit) error {
	return r.common.PersistExitState(containerID, exit)
}

func (r *RunscServiceHandler) waitLock(containerID string) *sync.Mutex {
	lock, _ := r.waitLocks.LoadOrStore(containerID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
