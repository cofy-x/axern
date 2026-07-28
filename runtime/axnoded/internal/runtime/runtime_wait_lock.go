package runtime

import "sync"

func startRuntimeWaitLocked(lock *sync.Mutex, start func() (<-chan error, error)) (<-chan error, error) {
	lock.Lock()
	wait, err := start()
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	completed := make(chan error, 1)
	go func() {
		err, _ := <-wait
		lock.Unlock()
		completed <- err
		close(completed)
	}()
	return completed, nil
}
