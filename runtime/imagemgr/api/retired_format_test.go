package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
)

func TestRetiredRoutesAreAbsent(t *testing.T) {
	worker := mustNewHttpWorker(t)
	for _, path := range []string{"/nydus_mount", "/nydus_umount", "/cleanup_daemon", "/list_daemons"} {
		response := httptest.NewRecorder()
		worker.prepareHttp().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("retired endpoint %s returned %d", path, response.Code)
		}
	}
}

func TestWorkerRejectsRetiredAuthorityBeforeReconciliation(t *testing.T) {
	store := openTestMountStore(t)
	record := &mountstore.Record{CacheKey: "nydus:retained", ImageURL: "retained", MountType: "nydus", NydusImageURL: "retained", MountPoint: "/retained"}
	if _, err := store.Acquire(record, "lease", "node"); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRelease("lease"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHttpWorker(&HttpWorkerConfig{MountStore: store}); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("retired authority was accepted: %v", err)
	}
	lease, err := store.GetLease("lease")
	if err != nil || lease == nil || !lease.Releasing || lease.Attempts != 1 {
		t.Fatalf("rejected authority changed cleanup debt: %+v %v", lease, err)
	}
}

func TestInventoryExportsOwnedImportedCacheIdentity(t *testing.T) {
	manager, err := oci.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	worker, err := NewHttpWorker(&HttpWorkerConfig{OCIManager: manager})
	if err != nil {
		t.Fatal(err)
	}
	ref := "registry.example/imported:one"
	archive, err := os.ReadFile(writeTestDockerArchive(t, ref))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ImportOCI(t.Context(), ref, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	resolved, err := worker.ResolveOCI(ref)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := worker.Inventory()
	if err != nil || len(inventory.ImportedImages) != 1 || inventory.ImportedImages[0].CacheKey != resolved.CacheKey || resolved.CacheKey == "" {
		t.Fatalf("inventory lost imported content identity: %+v, resolved=%+v, error=%v", inventory, resolved, err)
	}
}
