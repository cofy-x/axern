package api

import (
	"encoding/json"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHttpWorker_MountOCI_ImportedImageRetainsContentIdentity(t *testing.T) {

	ociMgr, err := oci.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("oci.NewManager() error: %v", err)
	}
	defer ociMgr.Close()

	imageURL := "example.local/imported-content:dev"
	result, err := ociMgr.ImportImageArchive(t.Context(), imageURL, writeTestDockerArchive(t, imageURL))
	if err != nil {
		t.Fatalf("ImportImageArchive() error: %v", err)
	}
	if err := os.Remove(result.ArchivePath); err != nil {
		t.Fatalf("remove imported archive: %v", err)
	}

	worker, err := NewHttpWorker(&HttpWorkerConfig{OCIManager: ociMgr})
	if err != nil {
		t.Fatalf("NewHttpWorker() error: %v", err)
	}
	worker.mountStore = openTestMountStore(t)

	_, err = worker.MountOCI(t.Context(), &OCIMountRequest{
		ImageURL: imageURL,
		LeaseID:  "test-import",
		Owner:    "test",
	})

	if err == nil {
		t.Fatal("MountOCI() error = nil, want missing imported archive error")
	}
	if !strings.Contains(err.Error(), "load imported content") {
		t.Fatalf("MountOCI() error = %q, want imported archive load error", err.Error())
	}
}

func TestHttpHandler_ListOCIMounts(t *testing.T) {
	t.Run("valid GET request", func(t *testing.T) {
		ociMgr, err := oci.NewManager(t.TempDir())
		if err != nil {
			t.Fatalf("oci.NewManager() error: %v", err)
		}
		defer ociMgr.Close()

		worker, err := NewHttpWorker(&HttpWorkerConfig{OCIManager: ociMgr})
		if err != nil {
			t.Fatalf("NewHttpWorker() error: %v", err)
		}
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mounts", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
		}

		var resp struct {
			ImageURLs []string `json:"image_urls"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.ImageURLs) != 0 {
			t.Fatalf("expected empty mounted OCI list, got %v", resp.ImageURLs)
		}
	})

	t.Run("invalid POST method", func(t *testing.T) {
		worker := mustNewHttpWorker(t)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodPost, "/list_oci_mounts", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("mount store remains authoritative without oci manager", func(t *testing.T) {
		worker := mustNewHttpWorker(t)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mounts", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestHttpHandler_ListOCIMountDetails(t *testing.T) {
	t.Run("valid GET request", func(t *testing.T) {
		ociMgr, err := oci.NewManager(t.TempDir())
		if err != nil {
			t.Fatalf("oci.NewManager() error: %v", err)
		}
		defer ociMgr.Close()

		worker, err := NewHttpWorker(&HttpWorkerConfig{OCIManager: ociMgr})
		if err != nil {
			t.Fatalf("NewHttpWorker() error: %v", err)
		}
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp struct {
			Mounts []MountedImageDetail `json:"mounts"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Mounts) != 0 {
			t.Fatalf("expected empty mount details, got %v", resp.Mounts)
		}
	})

	t.Run("mount store details include mount type", func(t *testing.T) {
		worker, err := NewHttpWorker(&HttpWorkerConfig{})
		if err != nil {
			t.Fatalf("NewHttpWorker() error: %v", err)
		}
		store, err := mountstore.Open(filepath.Join(t.TempDir(), "mounts.db"))
		if err != nil {
			t.Fatalf("mountstore.Open() error: %v", err)
		}
		defer store.Close()
		worker.mountStore = store
		record := newOCIMountRecord(&OCIMountRequest{
			ImageURL: "docker.io/library/alpine:latest",
		}, "/mnt/oci")
		if _, err := store.Acquire(record, "test-lease", "test"); err != nil {
			t.Fatal(err)
		}
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp struct {
			Mounts []MountedImageDetail `json:"mounts"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Mounts) != 1 {
			t.Fatalf("expected 1 mount detail, got %d", len(resp.Mounts))
		}
		if resp.Mounts[0].MountType != "oci" {
			t.Fatalf("MountType = %q, want %q", resp.Mounts[0].MountType, "oci")
		}
		if resp.Mounts[0].MountPath != "/mnt/oci" {
			t.Fatalf("MountPath = %q, want %q", resp.Mounts[0].MountPath, "/mnt/oci")
		}
	})

	t.Run("invalid POST method", func(t *testing.T) {
		worker := mustNewHttpWorker(t)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodPost, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("mount store remains authoritative without oci manager", func(t *testing.T) {
		worker := mustNewHttpWorker(t)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
		}
	})
}
