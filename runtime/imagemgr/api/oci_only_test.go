package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

func TestOCIOnlyEndpointsDoNotUseImagefsd(t *testing.T) {
	mgr := newMockManager()
	mgr.listDaemonsFunc = func() []imagefsd.DaemonInfo { t.Fatal("listed disabled imagefsd"); return nil }
	mgr.chunkDBStatsFunc = func() (*imagefsd.ChunkDBStats, error) { t.Fatal("queried disabled chunkdb"); return nil, nil }
	mgr.localityStatsFunc = func() (*imagefsd.LocalityStats, error) { t.Fatal("queried disabled locality"); return nil, nil }
	mgr.cleanupDaemonFunc = func(string) error { t.Fatal("cleaned disabled daemon"); return nil }
	worker, err := NewHttpWorker(&HttpWorkerConfig{DisableNydus: true, Manager: mgr})
	if err != nil {
		t.Fatal(err)
	}
	mux := worker.prepareHttp()
	for _, path := range []string{"/nydus_mount", "/nydus_umount", "/cleanup_daemon"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"image_url":"image","lease_id":"lease","owner":"test","daemon_id":"daemon"}`)))
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), ErrNydusDisabled.Error()) {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inventory", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"imagefsd_enabled":false`) {
		t.Fatalf("disabled inventory = %d %s", rec.Code, rec.Body.String())
	}
	var inventory InventoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Daemons) != 0 || inventory.ChunkDB != nil || inventory.ChunkDBError != "" || inventory.LocalityError != "" {
		t.Fatalf("disabled inventory queried backend: %+v", inventory)
	}
	worker, err = NewHttpWorker(&HttpWorkerConfig{Manager: newMockManager()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := worker.Inventory()
	if err != nil || !got.ImagefsdEnabled {
		t.Fatalf("zero-value configuration disabled Nydus: %+v, %v", got, err)
	}
}

func TestOCIOnlyWorkerRejectsRetainedNydusBeforeReconciliation(t *testing.T) {
	store := openTestMountStore(t)
	record := &mountstore.Record{CacheKey: "nydus:image", ImageURL: "image", NydusImageURL: "image", MountType: "nydus", MountPoint: "/mnt/image"}
	if _, err := store.Acquire(record, "lease", "test"); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRelease("lease"); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetLease("lease")
	if _, err := NewHttpWorker(&HttpWorkerConfig{DisableNydus: true, MountStore: store}); err == nil {
		t.Fatal("accepted retained Nydus authority")
	}
	after, _ := store.GetLease("lease")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected startup modified lease: before=%+v after=%+v", before, after)
	}
}

func TestOCIOnlyWorkerAllowsReleasingOCIResource(t *testing.T) {
	store := openTestMountStore(t)
	record := &mountstore.Record{CacheKey: "image", ImageURL: "image", MountType: "oci", MountPoint: "/mnt/image"}
	if _, err := store.Acquire(record, "lease", "test"); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRelease("lease"); err != nil {
		t.Fatal(err)
	}
	ociMgr, err := oci.NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer ociMgr.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	worker, err := NewHttpWorker(&HttpWorkerConfig{DisableNydus: true, MountStore: store, OCIManager: ociMgr, LifecycleContext: ctx})
	if err != nil {
		t.Fatal(err)
	}
	worker.reconcileReleasingMounts(t.Context())
	if lease, err := store.GetLease("lease"); err != nil || lease != nil {
		t.Fatalf("OCI release was not reconciled: %+v %v", lease, err)
	}
	if mount, err := store.GetMount(record.CacheKey); err != nil || mount != nil {
		t.Fatalf("released OCI resource remains: %+v %v", mount, err)
	}
}

func TestOCIOnlyPreservesNydusAuthorityOnAllLeaseRoutes(t *testing.T) {
	worker, err := NewHttpWorker(&HttpWorkerConfig{DisableNydus: true})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise defense in depth against unsupported state introduced after the
	// constructor, without running a background reconciler in this fixture.
	worker.mountStore = openTestMountStore(t)
	record := &mountstore.Record{CacheKey: "nydus:image", ImageURL: "image", NydusImageURL: "image", MountType: "nydus", MountPoint: "/mnt/image"}
	for _, id := range []string{"releasing", "active"} {
		if _, err := worker.mountStore.Acquire(record, id, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.mountStore.BeginRelease("releasing"); err != nil {
		t.Fatal(err)
	}
	before, _ := worker.mountStore.ListLeases()
	if err := worker.releaseLease("active", func(*mountstore.Record) error { t.Fatal("unmounted disabled backend"); return nil }); !errors.Is(err, ErrNydusDisabled) {
		t.Fatalf("releaseLease() = %v", err)
	}
	worker.reconcileReleasingMounts(t.Context())
	for _, desired := range [][]string{nil, {"active", "releasing"}} {
		if _, err := worker.ReconcileMountLeases(t.Context(), &ReconcileMountLeasesRequest{Owner: "test", LeaseIDs: desired}); !errors.Is(err, ErrNydusDisabled) {
			t.Fatalf("ReconcileMountLeases() = %v", err)
		}
	}
	if _, err := worker.acquireRecordedOCI(t.Context(), &OCIMountRequest{ImageURL: "image", LeaseID: "new", Owner: "test"}, record); !errors.Is(err, ErrNydusDisabled) {
		t.Fatalf("acquireRecordedOCI() = %v", err)
	}
	if err := worker.unmountNydusResource(t.Context(), "image"); !errors.Is(err, ErrNydusDisabled) {
		t.Fatalf("unmountNydusResource() = %v", err)
	}
	after, _ := worker.mountStore.ListLeases()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("disabled lease routes changed authority: before=%+v after=%+v", before, after)
	}
	got, _ := worker.mountStore.GetMount(record.CacheKey)
	if !reflect.DeepEqual(record, got) {
		t.Fatalf("disabled lease routes changed resource: %+v", got)
	}
}

func TestOCIOnlySkipsSuffixAndRejectsActualNydusWithInlineAuth(t *testing.T) {
	img, err := random.Image(128, 1)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Annotations = map[string]string{nydus.LayerAnnotationNydusBootstrap: "true"}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var suffixRequests, manifestRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "-nydus") {
			suffixRequests.Add(1)
		}
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/test/manifests/latest":
			manifestRequests.Add(1)
			w.Header().Set("Content-Type", string(types.DockerManifestSchema2))
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	t.Setenv("IMAGEMGR_INSECURE_REGISTRIES", host)
	registry, err := imageregistry.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	ociMgr, err := oci.NewManager(t.TempDir(), "", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer ociMgr.Close()
	worker, err := NewHttpWorker(&HttpWorkerConfig{
		DisableNydus: true, OCIManager: ociMgr,
		NydusClient: nydus.NewRegistryClientFromShared(registry), NydusSuffix: "-nydus",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.mountStore = openTestMountStore(t)
	for _, auth := range []string{"", fmt.Sprintf(`{"auths":{%q:{"username":"user","password":"secret"}}}`, host)} {
		_, err := worker.MountOCI(t.Context(), &OCIMountRequest{ImageURL: host + "/test:latest", DockerConfigJSON: auth, LeaseID: "lease", Owner: "test"})
		if !errors.Is(err, oci.ErrNydusImage) {
			t.Fatalf("MountOCI() accepted Nydus or used wrong route: %v", err)
		}
	}
	if suffixRequests.Load() != 0 || manifestRequests.Load() != 2 {
		t.Fatalf("unexpected detection requests: original=%d suffix=%d", manifestRequests.Load(), suffixRequests.Load())
	}
	if records, err := worker.mountStore.List(); err != nil || len(records) != 0 {
		t.Fatalf("Nydus failure published authority: %+v %v", records, err)
	}
}
