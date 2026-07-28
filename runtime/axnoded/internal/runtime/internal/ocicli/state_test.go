package ocicli

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPersistExitStateAllowsConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-exit-states", "axctl-test.json")

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func(status int) {
			defer wg.Done()
			errs <- PersistExitState(path, Exit{
				Timestamp: time.Now(),
				Status:    status,
			})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("PersistExitState() error = %v", err)
		}
	}
	if _, ok, err := ReadPersistedExitState(path, "test"); err != nil || !ok {
		t.Fatalf("ReadPersistedExitState() = ok %v, err %v", ok, err)
	}
}
