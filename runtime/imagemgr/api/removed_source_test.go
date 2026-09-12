package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

func TestRemovedOSSEndpointsReturnNotFound(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	for _, path := range []string{"/oss_mount", "/oss_umount"} {
		response := httptest.NewRecorder()
		worker.prepareHttp().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}

func TestWorkerRejectsLegacyMountWithoutDeletingRecord(t *testing.T) {
	store := openTestMountStore(t)
	if _, err := store.Acquire(&mountstore.Record{CacheKey: "legacy", MountType: "oss", MountPoint: "/mnt/legacy"}, "lease", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHttpWorker(&HttpWorkerConfig{Manager: newMockManager(), MountStore: store}); err == nil {
		t.Fatal("worker accepted legacy mount")
	}
	records, err := store.List()
	if err != nil || len(records) != 1 {
		t.Fatalf("legacy mount record changed: %v", err)
	}
}
