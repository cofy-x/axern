package api

import (
	"context"
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

func TestValidateOwnerRequiresReconcileScope(t *testing.T) {
	if err := validateOwner(""); err == nil {
		t.Fatal("validateOwner() allowed an ownerless durable lease")
	}
	if err := validateOwner("axnoded"); err != nil {
		t.Fatalf("validateOwner() rejected a valid owner: %v", err)
	}
}

func TestReleaseLeaseOnlyUnmountsFinalConsumer(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	record := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	if _, err := worker.mountStore.Acquire(record, "lease-1", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.mountStore.Acquire(record, "lease-2", "test"); err != nil {
		t.Fatal(err)
	}
	unmounts := 0
	unmount := func(*mountstore.Record) error { unmounts++; return nil }
	if err := worker.releaseLease("lease-1", unmount); err != nil {
		t.Fatal(err)
	}
	if unmounts != 0 {
		t.Fatalf("unmounts after non-final release = %d", unmounts)
	}
	if err := worker.releaseLease("lease-2", unmount); err != nil {
		t.Fatal(err)
	}
	if unmounts != 1 {
		t.Fatalf("unmounts after final release = %d, want 1", unmounts)
	}
}

func TestReleaseFailureRemainsDurableAndRetryable(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	record := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	if _, err := worker.mountStore.Acquire(record, "lease-1", "test"); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("busy")
	if err := worker.releaseLease("lease-1", func(*mountstore.Record) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("releaseLease() error = %v", err)
	}
	lease, err := worker.mountStore.GetLease("lease-1")
	if err != nil || lease == nil || !lease.Releasing || lease.LastError == "" {
		t.Fatalf("durable releasing lease = %+v, %v", lease, err)
	}
	if err := worker.releaseLease("lease-1", func(*mountstore.Record) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if lease, err := worker.mountStore.GetLease("lease-1"); err != nil || lease != nil {
		t.Fatalf("lease after retry = %+v, %v", lease, err)
	}
}

func TestReleaseTreatsAlreadyAbsentNydusResourceAsComplete(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	record := &mountstore.Record{CacheKey: "nydus:image", ImageURL: "image", NydusImageURL: "image", MountType: string(MountTypeNydus), MountPoint: "/mnt/image"}
	if _, err := worker.mountStore.Acquire(record, "lease-1", "test"); err != nil {
		t.Fatal(err)
	}
	if err := worker.releaseLease("lease-1", func(record *mountstore.Record) error {
		return worker.unmountNydusResource(context.Background(), record.NydusImageURL)
	}); err != nil {
		t.Fatal(err)
	}
	if lease, _ := worker.mountStore.GetLease("lease-1"); lease != nil {
		t.Fatalf("lease remains after idempotent resource release: %+v", lease)
	}
}

func TestReconcileMountLeasesRemovesOnlyStaleOwnerLeases(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	record := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	for _, item := range []struct{ id, owner string }{{"keep", "axnoded"}, {"stale", "axnoded"}, {"other", "operator"}} {
		if _, err := worker.mountStore.Acquire(record, item.id, item.owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.mountStore.BeginRelease("keep"); err != nil {
		t.Fatal(err)
	}
	resp, err := worker.ReconcileMountLeases(context.Background(), &ReconcileMountLeasesRequest{Owner: "axnoded", LeaseIDs: []string{"keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Retained != 1 || resp.Releasing != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if lease, _ := worker.mountStore.GetLease("stale"); lease != nil {
		t.Fatal("stale owner lease remains")
	}
	if lease, _ := worker.mountStore.GetLease("other"); lease == nil {
		t.Fatal("other owner lease was removed")
	}
	if lease, _ := worker.mountStore.GetLease("keep"); lease == nil || lease.Releasing {
		t.Fatalf("desired lease release intent was not cancelled: %+v", lease)
	}
}

func TestNydusRoutesShareCanonicalResourceIdentity(t *testing.T) {
	record := newNydusMountRecord(&OCIMountRequest{ImageURL: "registry.example/app:latest"}, "registry.example/app:latest-nydus", "/mnt/nydus")
	if got, want := record.CacheKey, "nydus:registry.example/app:latest-nydus"; got != want {
		t.Fatalf("Nydus resource key = %q, want %q", got, want)
	}
	if got, want := record.ImageURL, record.NydusImageURL; got != want {
		t.Fatalf("Nydus resource image = %q, want canonical image %q", got, want)
	}
}

func TestOCIInventoryExcludesOSSLeaseResources(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	ociRecord := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	ossRecord := &mountstore.Record{CacheKey: "oss:a", MountType: string(MountTypeOSS), MountPoint: "/mnt/oss"}
	if _, err := worker.mountStore.Acquire(ociRecord, "oci-lease", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.mountStore.Acquire(ossRecord, "oss-lease", "test"); err != nil {
		t.Fatal(err)
	}
	mounts, err := worker.ListMountedOCIDetails()
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].ImageURL != ociRecord.ImageURL {
		t.Fatalf("OCI mount details = %+v", mounts)
	}
}
