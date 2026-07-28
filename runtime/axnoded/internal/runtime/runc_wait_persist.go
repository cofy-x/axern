package runtime

import (
	"context"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RuncServiceHandler) startExitStatePersister(containerID string) error {
	go func() {
		waitLock := r.waitLock(containerID)
		waitLock.Lock()
		defer waitLock.Unlock()

		exit, ok, err := r.readExitState(containerID)
		if ok || err != nil {
			return
		}

		exit, ok, err = r.common.Wait(context.Background(), "wait", containerID)
		if err != nil {
			logrus.WithError(err).Warnf("runc exit-state persister failed for %s", containerID)
			return
		}
		if !ok {
			return
		}
		if err := r.persistExitState(containerID, contract.Exit{
			Timestamp: exit.Timestamp.UTC(),
			Status:    exit.Status,
		}); err != nil {
			logrus.WithError(err).Warnf("persist runc wait exit state failed for %s", containerID)
		}
	}()
	return nil
}

func (r *RuncServiceHandler) waitLock(containerID string) *sync.Mutex {
	lock, _ := r.waitLocks.LoadOrStore(containerID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (r *RuncServiceHandler) readExitState(containerID string) (contract.Exit, bool, error) {
	return r.common.ReadExitState(containerID, "runc")
}

func (r *RuncServiceHandler) persistExitState(containerID string, exit contract.Exit) error {
	return r.common.PersistExitState(containerID, exit)
}
