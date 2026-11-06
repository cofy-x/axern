package mountstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestValidateOCIOnlyFilePreservesRetainedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*bolt.Tx) error
		wantErr bool
	}{
		{name: "OCI releasing lease"},
		{name: "Nydus releasing lease", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Put([]byte("image"), []byte(`{"cache_key":"image","image_url":"image","mount_type":"nydus","nydus_image_url":"image","mount_point":"/mnt/image"}`))
		}},
		{name: "unknown source", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Put([]byte("image"), []byte(`{"cache_key":"image","mount_type":"unknown"}`))
		}},
		{name: "corrupt resource", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Put([]byte("image"), []byte(`{`))
		}},
		{name: "conflicting identity", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Put([]byte("image"), []byte(`{"cache_key":"other","image_url":"image","mount_type":"oci","mount_point":"/mnt/image"}`))
		}},
		{name: "OCI with Nydus identity", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Put([]byte("image"), []byte(`{"cache_key":"image","image_url":"image","mount_type":"oci","nydus_image_url":"image","mount_point":"/mnt/image"}`))
		}},
		{name: "corrupt lease", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(leasesBucket).Put([]byte("lease"), []byte(`{`))
		}},
		{name: "orphan lease", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(mountsBucket).Delete([]byte("image"))
		}},
		{name: "missing authority bucket", wantErr: true, mutate: func(tx *bolt.Tx) error {
			return tx.DeleteBucket(leasesBucket)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resource_leases.db")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			record := &Record{CacheKey: "image", ImageURL: "image", MountType: "oci", MountPoint: "/mnt/image"}
			if _, err := store.Acquire(record, "lease", "node"); err != nil {
				t.Fatal(err)
			}
			if err := store.BeginRelease("lease"); err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				if err := store.db.Update(tc.mutate); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.ValidateOCIOnly(); (err != nil) != tc.wantErr {
				t.Fatalf("ValidateOCIOnly() = %v, wantErr=%t", err, tc.wantErr)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateOCIOnlyFile(path); (err != nil) != tc.wantErr {
				t.Fatalf("ValidateOCIOnlyFile() = %v, wantErr=%t", err, tc.wantErr)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("validation modified retained authority: %v", err)
			}
		})
	}
}

func TestValidateOCIOnlyFileMissingAndCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource_leases.db")
	if err := ValidateOCIOnlyFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("validation created missing store: %v", err)
	}
	data := []byte("not a Bolt database")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOCIOnlyFile(path); err == nil {
		t.Fatal("corrupt database accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("corrupt database changed: %v", err)
	}
}
