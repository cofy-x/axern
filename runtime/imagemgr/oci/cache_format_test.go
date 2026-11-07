package oci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestExtractionFormatInitializesEmptyCacheAndReopensRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.db")
	store, err := openMetadataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.putLayer(&LayerRecord{Digest: "sha256:example", Path: "/retained", RefCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openMetadataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	record, err := store.getLayer("sha256:example")
	if err != nil || record == nil || record.RefCount != 1 {
		t.Fatalf("retained layer = %+v, %v", record, err)
	}
}

func TestExtractionFormatAcceptsUnversionedEmptyBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket(layerRecordsBucket)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := openMetadataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractionFormatRejectsUnversionedRecordsBeforeMountRecovery(t *testing.T) {
	for _, bucketName := range [][]byte{layerRecordsBucket, chainRecordsBucket, ociMountStateBucket, mountTxnBucket, importContentsBucket} {
		t.Run(string(bucketName), func(t *testing.T) {
			work := t.TempDir()
			root := filepath.Join(work, "oci")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "metadata.db")
			db, err := bolt.Open(path, 0600, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucket(bucketName)
				if err != nil {
					return err
				}
				return bucket.Put([]byte("owned-record"), []byte("retained original bytes"))
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := NewManager(work, ""); err == nil || !strings.Contains(err.Error(), "unversioned") {
				t.Fatalf("NewManager() = %v, want unversioned cache rejection", err)
			}
			if _, err := os.Stat(filepath.Join(root, "support")); !os.IsNotExist(err) {
				t.Fatalf("rejected cache mutated support state: %v", err)
			}
			db, err = bolt.Open(path, 0600, &bolt.Options{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.View(func(tx *bolt.Tx) error {
				if tx.Bucket(extractionFormatBucket) != nil ||
					string(tx.Bucket(bucketName).Get([]byte("owned-record"))) != "retained original bytes" {
					t.Fatal("rejected cache was stamped or its records changed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExtractionFormatRejectsUnversionedCrashContent(t *testing.T) {
	root := t.TempDir()
	layer := filepath.Join(root, "layers", "old-layer", "fs")
	if err := os.MkdirAll(layer, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := openMetadataStore(filepath.Join(root, "metadata.db")); err == nil || !strings.Contains(err.Error(), "unversioned") {
		t.Fatalf("openMetadataStore() = %v, want orphan cache rejection", err)
	}
	assertDirectoryMode(t, layer, 0700)
}

func TestExtractionFormatRejectsUnknownOrMalformedVersion(t *testing.T) {
	for _, version := range []string{"", "future-format"} {
		t.Run(version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "metadata.db")
			store, err := openMetadataStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket(extractionFormatBucket).Put([]byte("version"), []byte(version))
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
			if _, err := openMetadataStore(path); err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("openMetadataStore() = %v, want unsupported format", err)
			}
		})
	}
}
