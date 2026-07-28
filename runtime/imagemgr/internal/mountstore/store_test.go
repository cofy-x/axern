package mountstore

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "mounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestAcquireIsIdempotentAndRejectsLeaseRebind(t *testing.T) {
	store := openTestStore(t)
	record := &Record{CacheKey: "image:a", ImageURL: "image:a", MountType: "oci", MountPoint: "/mnt/a"}
	created, err := store.Acquire(record, "lease-1", "node")
	if err != nil || !created {
		t.Fatalf("Acquire() = %v, %v", created, err)
	}
	created, err = store.Acquire(record, "lease-1", "node")
	if err != nil || created {
		t.Fatalf("idempotent Acquire() = %v, %v", created, err)
	}
	if _, err := store.Acquire(&Record{CacheKey: "image:b", ImageURL: "image:b", MountType: "oci", MountPoint: "/mnt/b"}, "lease-1", "node"); err == nil {
		t.Fatal("Acquire() allowed lease rebind")
	}
	if _, err := store.Acquire(record, "lease-1", "other"); err == nil {
		t.Fatal("Acquire() allowed lease owner change")
	}
	count, err := store.LeaseCount(record.CacheKey)
	if err != nil || count != 1 {
		t.Fatalf("LeaseCount() = %d, %v", count, err)
	}
}

func TestBucketNamesAreUnversionedResourceTerms(t *testing.T) {
	if got, want := string(mountsBucket), "mount_resources"; got != want {
		t.Fatalf("mount bucket = %q, want %q", got, want)
	}
	if got, want := string(leasesBucket), "mount_leases"; got != want {
		t.Fatalf("lease bucket = %q, want %q", got, want)
	}
}

func TestReleaseRequiresLastLeaseBeforeMountRemoval(t *testing.T) {
	store := openTestStore(t)
	record := &Record{CacheKey: "image:a", ImageURL: "image:a", MountType: "oci", MountPoint: "/mnt/a"}
	if _, err := store.Acquire(record, "lease-1", "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(record, "lease-2", "node"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseLease("lease-1", true); err == nil {
		t.Fatal("ReleaseLease() removed mount with another active lease")
	}
	if err := store.ReleaseLease("lease-1", false); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseLease("lease-2", true); err != nil {
		t.Fatal(err)
	}
	if mount, err := store.GetMount(record.CacheKey); err != nil || mount != nil {
		t.Fatalf("GetMount() = %+v, %v", mount, err)
	}
}

func TestStoreReopensDurableLeaseGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mounts.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	record := &Record{CacheKey: "image:a", ImageURL: "image:a", MountType: "nydus", MountPoint: "/mnt/a"}
	if _, err := store.Acquire(record, "lease-1", "node"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease, err := store.GetLease("lease-1")
	if err != nil || lease == nil || lease.MountKey != record.CacheKey {
		t.Fatalf("GetLease() = %+v, %v", lease, err)
	}
	mount, err := store.GetMount(record.CacheKey)
	if err != nil || mount == nil || mount.MountPoint != record.MountPoint {
		t.Fatalf("GetMount() = %+v, %v", mount, err)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("Open() succeeded for directory path")
	}
}
