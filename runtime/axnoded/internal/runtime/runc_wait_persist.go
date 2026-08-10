package runtime

import (
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

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
