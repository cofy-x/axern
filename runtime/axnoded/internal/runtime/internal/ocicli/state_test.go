package ocicli

import (
	"os"
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

func TestPersistExitStateRejectsCorruptOrNonRegularExistingState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exit.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PersistExitState(path, Exit{Timestamp: time.Now().UTC(), Status: 1}); err == nil {
		t.Fatal("corrupt existing exit state was accepted as durable")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", path); err != nil {
		t.Fatal(err)
	}
	if err := PersistExitState(path, Exit{Timestamp: time.Now().UTC(), Status: 1}); err == nil {
		t.Fatal("symlink exit state was accepted as durable")
	}
}
