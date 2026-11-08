package api

import (
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"path/filepath"
	"testing"
)

func mustNewHttpWorker(t *testing.T) *HttpWorker {
	t.Helper()
	w, err := NewHttpWorker(&HttpWorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	w.mountStore = openTestMountStore(t)
	return w
}
func openTestMountStore(t *testing.T) *mountstore.Store {
	t.Helper()
	store, err := mountstore.Open(filepath.Join(t.TempDir(), "mounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
