package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func TestHttpHandler_OCIImport(t *testing.T) {
	mgr := newMockManager()
	ociMgr, err := oci.NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("oci.NewManager() error: %v", err)
	}
	defer ociMgr.Close()

	worker, err := NewHttpWorker(&HttpWorkerConfig{
		Manager:    mgr,
		OCIManager: ociMgr,
	})
	if err != nil {
		t.Fatalf("NewHttpWorker() error: %v", err)
	}
	handler := worker.prepareHttp()

	imageRef := "example.local/myapp:dev"
	archivePath := writeTestDockerArchive(t, imageRef)
	body, _ := json.Marshal(OCIImportRequest{
		ArchivePath: archivePath,
		ImageRef:    imageRef,
	})
	req := httptest.NewRequest(http.MethodPost, "/oci_import", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp OCIImportResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ImageRef != imageRef {
		t.Fatalf("ImageRef = %q, want %q", resp.ImageRef, imageRef)
	}
	if resp.SizeBytes == 0 {
		t.Fatal("SizeBytes = 0, want non-zero")
	}
	if !strings.HasPrefix(resp.ArchiveDigest, "sha256:") {
		t.Fatalf("ArchiveDigest = %q, want sha256 digest", resp.ArchiveDigest)
	}

	inventory, err := worker.Inventory()
	if err != nil {
		t.Fatalf("Inventory() error: %v", err)
	}
	if len(inventory.ImportedImages) != 1 {
		t.Fatalf("ImportedImages length = %d, want 1", len(inventory.ImportedImages))
	}
	if inventory.ImportedImages[0].ImageRef != imageRef {
		t.Fatalf("ImportedImages[0].ImageRef = %q, want %q", inventory.ImportedImages[0].ImageRef, imageRef)
	}
	if inventory.ImportedImages[0].ArchiveDigest != resp.ArchiveDigest {
		t.Fatalf("ImportedImages[0].ArchiveDigest = %q, want %q", inventory.ImportedImages[0].ArchiveDigest, resp.ArchiveDigest)
	}
}

func TestHttpHandler_OCIImportPreservesMountedRecord(t *testing.T) {
	mgr := newMockManager()
	ociMgr, err := oci.NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("oci.NewManager() error: %v", err)
	}
	defer ociMgr.Close()

	worker, err := NewHttpWorker(&HttpWorkerConfig{
		Manager:    mgr,
		OCIManager: ociMgr,
	})
	if err != nil {
		t.Fatalf("NewHttpWorker() error: %v", err)
	}
	store, err := mountstore.Open(filepath.Join(t.TempDir(), "mounts.db"))
	if err != nil {
		t.Fatalf("mountstore.Open() error: %v", err)
	}
	defer store.Close()
	worker.mountStore = store

	imageRef := "example.local/mounted-import:dev"
	if _, err := worker.mountStore.Acquire(&mountstore.Record{
		CacheKey:   imageRef,
		ImageURL:   imageRef,
		MountType:  string(MountTypeOCI),
		MountPoint: "/mnt/existing",
	}, "test-lease", "test"); err != nil {
		t.Fatalf("Acquire() mount record error: %v", err)
	}

	handler := worker.prepareHttp()
	body, _ := json.Marshal(OCIImportRequest{
		ArchivePath: writeTestDockerArchive(t, imageRef),
		ImageRef:    imageRef,
	})
	req := httptest.NewRequest(http.MethodPost, "/oci_import", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	record, err := worker.mountStore.GetMount(imageRef)
	if err != nil {
		t.Fatalf("Get() mount record error: %v", err)
	}
	if record == nil || record.MountPoint != "/mnt/existing" {
		t.Fatalf("mount record after import = %+v, want existing mount", record)
	}
}

func TestHttpHandler_OCIImportValidation(t *testing.T) {
	mgr := newMockManager()
	worker := mustNewHttpWorker(t, mgr)
	handler := worker.prepareHttp()

	req := httptest.NewRequest(http.MethodPost, "/oci_import", strings.NewReader(`{"archive_path":"/tmp/nope.tar"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "oci manager is not initialized") {
		t.Fatalf("body = %q, want oci manager error", w.Body.String())
	}
}

func writeTestDockerArchive(t *testing.T, imageRef string) string {
	t.Helper()
	img, err := random.Image(128, 1)
	if err != nil {
		t.Fatalf("random.Image() error: %v", err)
	}
	ref, err := name.ParseReference(imageRef, name.WeakValidation)
	if err != nil {
		t.Fatalf("ParseReference() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := tarball.WriteToFile(path, ref, img); err != nil {
		t.Fatalf("WriteToFile() error: %v", err)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Fatalf("archive stat size = %v, err = %v", st, err)
	}
	return path
}
