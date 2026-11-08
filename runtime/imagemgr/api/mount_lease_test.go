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

func TestReleaseScanCannotDeleteRetainedLease(t *testing.T) {
	worker := mustNewHttpWorker(t)
	record := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	if _, err := worker.mountStore.Acquire(record, "lease", "axnoded"); err != nil {
		t.Fatal(err)
	}
	if err := worker.mountStore.BeginRelease("lease"); err != nil {
		t.Fatal(err)
	}
	candidates, err := worker.mountStore.ListReleasing()
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
	if err := worker.mountStore.RetainLease("lease", "axnoded"); err != nil {
		t.Fatal(err)
	}
	worker.retryReleasingMount(t.Context(), candidates[0])
	lease, err := worker.mountStore.GetLease("lease")
	if err != nil || lease == nil || lease.Releasing {
		t.Fatalf("retained lease=%v err=%v", lease, err)
	}
}

func TestReleaseRetryDropsOnlyRetiringSharedLease(t *testing.T) {
	worker := mustNewHttpWorker(t)
	record := &mountstore.Record{CacheKey: "image:a", ImageURL: "image:a", MountType: string(MountTypeOCI), MountPoint: "/mnt/a"}
	for _, id := range []string{"retiring", "active"} {
		if _, err := worker.mountStore.Acquire(record, id, "axnoded"); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.mountStore.BeginRelease("retiring"); err != nil {
		t.Fatal(err)
	}
	worker.reconcileReleasingMounts(t.Context())
	if lease, err := worker.mountStore.GetLease("retiring"); err != nil || lease != nil {
		t.Fatalf("retiring lease=%v err=%v", lease, err)
	}
	if lease, err := worker.mountStore.GetLease("active"); err != nil || lease == nil {
		t.Fatalf("active lease=%v err=%v", lease, err)
	}
}

func TestReleaseRetryPreservesUnknownFormatWithoutMutation(t *testing.T) {
	worker := mustNewHttpWorker(t)
	// An unsupported backend fails deterministically without kernel side effects.
	record := &mountstore.Record{CacheKey: "invalid", MountType: "invalid", MountPoint: "/mnt/invalid"}
	if _, err := worker.mountStore.Acquire(record, "lease", "axnoded"); err != nil {
		t.Fatal(err)
	}
	if err := worker.mountStore.BeginRelease("lease"); err != nil {
		t.Fatal(err)
	}
	worker.reconcileReleasingMounts(t.Context())
	lease, err := worker.mountStore.GetLease("lease")
	if err != nil || lease == nil || !lease.Releasing || lease.Attempts != 1 {
		t.Fatalf("release retry diagnostics = %+v, err=%v", lease, err)
	}
}

func TestReleaseLeaseOnlyUnmountsFinalConsumer(t *testing.T) {
	worker := mustNewHttpWorker(t)
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
	worker := mustNewHttpWorker(t)
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

func TestReconcileMountLeasesRemovesOnlyStaleOwnerLeases(t *testing.T) {
	worker := mustNewHttpWorker(t)
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

func TestOCIInventoryRejectsInvalidLeaseResources(t *testing.T) {
	worker := mustNewHttpWorker(t)
	record := &mountstore.Record{CacheKey: "invalid:a", MountType: "invalid", MountPoint: "/mnt/invalid"}
	if _, err := worker.mountStore.Acquire(record, "invalid-lease", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ListMountedOCIDetails(); err == nil {
		t.Fatal("unsupported persisted mount type must be rejected")
	}
	if _, err := worker.ListMountedOCIImages(); err == nil {
		t.Fatal("unsupported persisted mount type must be rejected")
	}
}
