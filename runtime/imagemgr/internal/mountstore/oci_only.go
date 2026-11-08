package mountstore

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ValidateOCIOnlyFile inspects existing authority before any backend recovery.
// A missing store is allowed; an existing store is never created or modified.
func ValidateOCIOnlyFile(dbPath string) error {
	info, err := os.Lstat(dbPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect mount store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("mount store %s is not a regular file", dbPath)
	}
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		return fmt.Errorf("inspect mount store: %w", err)
	}
	defer db.Close()
	return db.View(validateOCIOnly)
}

// ValidateOCIOnly also protects workers constructed outside the daemon wiring.
func (s *Store) ValidateOCIOnly() error {
	return s.db.View(validateOCIOnly)
}

func validateOCIOnly(tx *bolt.Tx) error {
	mounts, leases := tx.Bucket(mountsBucket), tx.Bucket(leasesBucket)
	if mounts == nil || leases == nil {
		return fmt.Errorf("mount store is missing authoritative resource or lease records")
	}
	if err := mounts.ForEach(func(key, raw []byte) error {
		var record Record
		if err := json.Unmarshal(raw, &record); err != nil {
			return fmt.Errorf("invalid persisted mount %q: %w", key, err)
		}
		if record.MountType != "oci" {
			return fmt.Errorf("persisted mount %q has type %q; retired or unsupported image format; only OCI resources are accepted", key, record.MountType)
		}
		if record.CacheKey != string(key) || record.ImageURL == "" || record.MountPoint == "" || record.NydusImageURL != "" {
			return fmt.Errorf("invalid persisted OCI mount identity %q", key)
		}
		return nil
	}); err != nil {
		return err
	}
	return leases.ForEach(func(key, raw []byte) error {
		var lease Lease
		if err := json.Unmarshal(raw, &lease); err != nil {
			return fmt.Errorf("invalid persisted lease %q: %w", key, err)
		}
		if lease.ID == "" || lease.ID != string(key) || lease.MountKey == "" || mounts.Get([]byte(lease.MountKey)) == nil {
			return fmt.Errorf("invalid persisted lease identity or missing resource for %q", key)
		}
		return nil
	})
}
