package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
)

func TestHttpWorker_MountOCI_NydusMountFailureDoesNotFallback(t *testing.T) {
	t.Run("detected on original image URL", func(t *testing.T) {
		mgr := newMockManager()
		expectedErr := errors.New("mock nydus create failure")
		var gotImageURL string
		mgr.createDaemonFunc = func(opts *imagefsd.DaemonCreateOpt) error {
			gotImageURL = opts.ImageURL
			if opts.SourceType != "nydus" {
				t.Fatalf("SourceType = %q, want %q", opts.SourceType, "nydus")
			}
			return expectedErr
		}

		ociMgr, err := oci.NewManager(t.TempDir(), "")
		if err != nil {
			t.Fatalf("oci.NewManager() error: %v", err)
		}
		defer ociMgr.Close()

		worker, err := NewHttpWorker(&HttpWorkerConfig{
			Manager:     mgr,
			OCIManager:  ociMgr,
			NydusClient: &nydus.RegistryClient{},
		})
		if err != nil {
			t.Fatalf("NewHttpWorker() error: %v", err)
		}
		worker.mountStore = openTestMountStore(t)

		imageURL := "%%%original"
		worker.nydusCache.set(imageURL, true)

		resp, err := worker.MountOCI(t.Context(), &OCIMountRequest{ImageURL: imageURL, LeaseID: "test-original", Owner: "test"})
		if err == nil {
			t.Fatal("MountOCI() error = nil, want non-nil")
		}
		if resp != nil {
			t.Fatalf("MountOCI() resp = %v, want nil", resp)
		}
		if !strings.Contains(err.Error(), "failed to mount Nydus image") {
			t.Fatalf("MountOCI() error = %q, want Nydus mount failure", err.Error())
		}
		if !strings.Contains(err.Error(), expectedErr.Error()) {
			t.Fatalf("MountOCI() error = %q, want underlying create error", err.Error())
		}
		if gotImageURL != imageURL {
			t.Fatalf("CreateDaemon imageURL = %q, want %q", gotImageURL, imageURL)
		}
	})

	t.Run("detected on suffix image URL", func(t *testing.T) {
		mgr := newMockManager()
		expectedErr := errors.New("mock suffix nydus create failure")
		var gotImageURL string
		mgr.createDaemonFunc = func(opts *imagefsd.DaemonCreateOpt) error {
			gotImageURL = opts.ImageURL
			if opts.SourceType != "nydus" {
				t.Fatalf("SourceType = %q, want %q", opts.SourceType, "nydus")
			}
			return expectedErr
		}

		ociMgr, err := oci.NewManager(t.TempDir(), "")
		if err != nil {
			t.Fatalf("oci.NewManager() error: %v", err)
		}
		defer ociMgr.Close()

		worker, err := NewHttpWorker(&HttpWorkerConfig{
			Manager:     mgr,
			OCIManager:  ociMgr,
			NydusClient: &nydus.RegistryClient{},
			NydusSuffix: "-nydus",
		})
		if err != nil {
			t.Fatalf("NewHttpWorker() error: %v", err)
		}
		worker.mountStore = openTestMountStore(t)

		imageURL := "%%%base"
		suffixedImageURL := imageURL + "-nydus"
		worker.nydusCache.set(imageURL, false)
		worker.nydusCache.set(suffixedImageURL, true)

		resp, err := worker.MountOCI(t.Context(), &OCIMountRequest{ImageURL: imageURL, LeaseID: "test-suffix", Owner: "test"})
		if err == nil {
			t.Fatal("MountOCI() error = nil, want non-nil")
		}
		if resp != nil {
			t.Fatalf("MountOCI() resp = %v, want nil", resp)
		}
		if !strings.Contains(err.Error(), "failed to mount Nydus image") {
			t.Fatalf("MountOCI() error = %q, want Nydus mount failure", err.Error())
		}
		if !strings.Contains(err.Error(), expectedErr.Error()) {
			t.Fatalf("MountOCI() error = %q, want underlying create error", err.Error())
		}
		if gotImageURL != suffixedImageURL {
			t.Fatalf("CreateDaemon imageURL = %q, want %q", gotImageURL, suffixedImageURL)
		}
	})
}

func TestHttpWorker_MountOCI_ImportedImageSkipsNydusDetection(t *testing.T) {
	mgr := newMockManager()
	mgr.createDaemonFunc = func(opts *imagefsd.DaemonCreateOpt) error {
		t.Fatalf("CreateDaemon called for imported image %s", opts.ImageURL)
		return nil
	}

	ociMgr, err := oci.NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("oci.NewManager() error: %v", err)
	}
	defer ociMgr.Close()

	imageURL := "example.local/imported-skip-nydus:dev"
	result, err := ociMgr.ImportImageArchive(t.Context(), imageURL, writeTestDockerArchive(t, imageURL))
	if err != nil {
		t.Fatalf("ImportImageArchive() error: %v", err)
	}
	if err := os.Remove(result.ArchivePath); err != nil {
		t.Fatalf("remove imported archive: %v", err)
	}

	worker, err := NewHttpWorker(&HttpWorkerConfig{
		Manager:     mgr,
		OCIManager:  ociMgr,
		NydusClient: &nydus.RegistryClient{},
	})
	if err != nil {
		t.Fatalf("NewHttpWorker() error: %v", err)
	}
	worker.mountStore = openTestMountStore(t)
	worker.nydusCache.set(imageURL, true)

	_, err = worker.MountOCI(t.Context(), &OCIMountRequest{
		ImageURL: imageURL,
		CacheKey: imageURL + "@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		LeaseID:  "test-import",
		Owner:    "test",
	})

	if err == nil {
		t.Fatal("MountOCI() error = nil, want missing imported archive error")
	}
	if strings.Contains(err.Error(), "requested stale imported image generation") {
		t.Fatalf("MountOCI() error = %q, want API to resolve current imported generation", err.Error())
	}
	if !strings.Contains(err.Error(), "load imported image archive") {
		t.Fatalf("MountOCI() error = %q, want imported archive load error", err.Error())
	}
}

func TestHttpWorker_DetectNydusOnce_PreservesError(t *testing.T) {
	worker := &HttpWorker{}
	expectedErr := errors.New("mock nydus detection failure")

	detected, err := worker.detectNydusOnce(t.Context(), "test-image", func(context.Context) (bool, error) {
		return false, expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("detectNydusOnce() error = %v, want %v", err, expectedErr)
	}
	if detected {
		t.Fatal("detectNydusOnce() detected = true on error")
	}
}

func TestHttpWorker_DetectNydusOnce_RequestCancellationDoesNotCancelSharedWork(t *testing.T) {
	worker := &HttpWorker{lifecycleCtx: t.Context()}
	started := make(chan struct{})
	release := make(chan struct{})
	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	leaderDone := make(chan error, 1)

	go func() {
		_, err := worker.detectNydusOnce(leaderCtx, "test-image", func(ctx context.Context) (bool, error) {
			close(started)
			select {
			case <-release:
				return true, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		})
		leaderDone <- err
	}()

	<-started
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context canceled", err)
	}

	followerDone := make(chan struct {
		detected bool
		err      error
	}, 1)
	go func() {
		detected, err := worker.detectNydusOnce(t.Context(), "test-image", func(context.Context) (bool, error) {
			return false, errors.New("singleflight follower started duplicate work")
		})
		followerDone <- struct {
			detected bool
			err      error
		}{detected: detected, err: err}
	}()

	select {
	case result := <-followerDone:
		t.Fatalf("follower returned before shared work completed: %v", result.err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	result := <-followerDone
	if result.err != nil {
		t.Fatalf("follower error = %v", result.err)
	}
	if !result.detected {
		t.Fatal("follower did not receive shared result")
	}
}

func TestNydusDetectionKeySeparatesRequestCredentials(t *testing.T) {
	imageURL := "registry.example/repo/image:latest"
	first := nydusDetectionKey(imageURL, `{"auths":{"registry.example":{"auth":"first"}}}`)
	second := nydusDetectionKey(imageURL, `{"auths":{"registry.example":{"auth":"second"}}}`)

	if first == second {
		t.Fatal("nydusDetectionKey() coalesced requests with different credentials")
	}
	if first != nydusDetectionKey(imageURL, `{"auths":{"registry.example":{"auth":"first"}}}`) {
		t.Fatal("nydusDetectionKey() is not stable")
	}
}

func TestTryMountNydusDoesNotMountAfterRequestCancellation(t *testing.T) {
	mgr := newMockManager()
	worker, err := NewHttpWorker(&HttpWorkerConfig{
		Manager:     mgr,
		NydusClient: &nydus.RegistryClient{},
	})
	if err != nil {
		t.Fatalf("NewHttpWorker() error: %v", err)
	}
	imageURL := "registry.example/repo/image:latest"
	worker.nydusCache.set(imageURL, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	attempt, err := worker.tryMountNydus(ctx, imageURL, "")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("tryMountNydus() error = %v, want context canceled", err)
	}
	if !attempt.detected {
		t.Fatal("tryMountNydus() lost detected state on cancellation")
	}
	if len(mgr.daemons) != 0 {
		t.Fatalf("tryMountNydus() created %d daemon(s) after cancellation", len(mgr.daemons))
	}
}

func TestHttpHandler_ListOCIMounts(t *testing.T) {
	t.Run("valid GET request", func(t *testing.T) {
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
		mgr := newMockManager()
		worker := mustNewHttpWorker(t, mgr)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodPost, "/list_oci_mounts", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("mount store remains authoritative without oci manager", func(t *testing.T) {
		mgr := newMockManager()
		worker := mustNewHttpWorker(t, mgr)
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
		mgr := newMockManager()
		worker, err := NewHttpWorker(&HttpWorkerConfig{
			Manager: mgr,
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
		record := newNydusMountRecord(&OCIMountRequest{
			ImageURL: "docker.io/library/alpine:latest",
		}, "docker.io/library/alpine:latest-nydus", "/mnt/nydus")
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
		if resp.Mounts[0].MountType != "nydus" {
			t.Fatalf("MountType = %q, want %q", resp.Mounts[0].MountType, "nydus")
		}
		if resp.Mounts[0].MountPath != "/mnt/nydus" {
			t.Fatalf("MountPath = %q, want %q", resp.Mounts[0].MountPath, "/mnt/nydus")
		}
	})

	t.Run("invalid POST method", func(t *testing.T) {
		mgr := newMockManager()
		worker := mustNewHttpWorker(t, mgr)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodPost, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("mount store remains authoritative without oci manager", func(t *testing.T) {
		mgr := newMockManager()
		worker := mustNewHttpWorker(t, mgr)
		handler := worker.prepareHttp()

		req := httptest.NewRequest(http.MethodGet, "/list_oci_mount_details", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
		}
	})
}
