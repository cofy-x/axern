package oci

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

var extractionFormatBucket = []byte("extraction_format")

const extractionFormat = "guest-metadata-v1"

// The metadata DB owns one extraction format for all cached layers, chains,
// mounted projections and crash-recovery records. Never repair leased content
// or stamp an existing unversioned cache as if it had been extracted correctly.
func initializeExtractionFormat(tx *bolt.Tx, cacheRoot string) error {
	key := []byte("version")
	if bucket := tx.Bucket(extractionFormatBucket); bucket != nil {
		if !bytes.Equal(bucket.Get(key), []byte(extractionFormat)) || bucket.Stats().KeyN != 1 {
			return fmt.Errorf("unsupported OCI extraction cache format; preserve this cache and resolve it offline after releasing its leases with the matching runtime")
		}
		return nil
	}
	if err := tx.ForEach(func(_ []byte, bucket *bolt.Bucket) error {
		if key, _ := bucket.Cursor().First(); key != nil {
			return fmt.Errorf("unversioned OCI extraction cache contains records; preserve it and release all leases with the matching runtime before offline cache replacement")
		}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range []string{"layers", "lowerdirs", "mounts", "imports"} {
		entries, err := os.ReadDir(filepath.Join(cacheRoot, name))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect unversioned OCI cache %s: %w", name, err)
		}
		if len(entries) != 0 {
			return fmt.Errorf("unversioned OCI extraction cache contains %s; preserve it and resolve the offline cache before starting this runtime", name)
		}
	}
	bucket, err := tx.CreateBucket(extractionFormatBucket)
	if err != nil {
		return err
	}
	return bucket.Put(key, []byte(extractionFormat))
}
