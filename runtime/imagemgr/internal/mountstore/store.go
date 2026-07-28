package mountstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	mountsBucket = []byte("mount_resources")
	leasesBucket = []byte("mount_leases")
)

// Record is the durable mount resource. CacheKey is its identity.
type Record struct {
	CacheKey      string `json:"cache_key"`
	ImageURL      string `json:"image_url"`
	MountType     string `json:"mount_type"`
	NydusImageURL string `json:"nydus_image_url,omitempty"`
	MountPoint    string `json:"mount_point"`
	Endpoint      string `json:"endpoint,omitempty"`
	Bucket        string `json:"bucket,omitempty"`
	Object        string `json:"object,omitempty"`
}

// Lease is one durable consumer of a mount resource.
type Lease struct {
	ID        string `json:"id"`
	MountKey  string `json:"mount_key"`
	Owner     string `json:"owner,omitempty"`
	CreatedAt int64  `json:"created_at_unix"`
	UpdatedAt int64  `json:"updated_at_unix"`
	Releasing bool   `json:"releasing,omitempty"`
	Attempts  int    `json:"release_attempts,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type Store struct {
	db  *bolt.DB
	now func() time.Time
}

func Open(dbPath string) (*Store, error) {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open mount store: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(mountsBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(leasesBucket)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create mount store buckets: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Acquire atomically records the mount resource and one idempotent lease.
// A lease ID may never be rebound to a different mount resource.
func (s *Store) Acquire(record *Record, leaseID, owner string) (bool, error) {
	if record == nil || record.CacheKey == "" || leaseID == "" {
		return false, fmt.Errorf("mount record, cache key, and lease id are required")
	}
	created := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		mounts := tx.Bucket(mountsBucket)
		leases := tx.Bucket(leasesBucket)
		if raw := leases.Get([]byte(leaseID)); raw != nil {
			var existing Lease
			if err := json.Unmarshal(raw, &existing); err != nil {
				return err
			}
			if existing.MountKey != record.CacheKey {
				return fmt.Errorf("lease %q already owns mount %q", leaseID, existing.MountKey)
			}
			if existing.Owner != owner {
				return fmt.Errorf("lease %q is owned by %q", leaseID, existing.Owner)
			}
			if existing.Releasing {
				existing.Releasing = false
				existing.LastError = ""
				existing.UpdatedAt = s.now().UTC().Unix()
				data, err := json.Marshal(&existing)
				if err != nil {
					return err
				}
				return leases.Put([]byte(leaseID), data)
			}
			return nil
		}
		if raw := mounts.Get([]byte(record.CacheKey)); raw != nil {
			var existing Record
			if err := json.Unmarshal(raw, &existing); err != nil {
				return err
			}
			if existing.MountType != record.MountType || existing.MountPoint != record.MountPoint || existing.ImageURL != record.ImageURL {
				return fmt.Errorf("mount resource %q conflicts with persisted identity", record.CacheKey)
			}
		} else {
			data, err := json.Marshal(record)
			if err != nil {
				return err
			}
			if err := mounts.Put([]byte(record.CacheKey), data); err != nil {
				return err
			}
		}
		now := s.now().UTC().Unix()
		lease := Lease{ID: leaseID, MountKey: record.CacheKey, Owner: owner, CreatedAt: now, UpdatedAt: now}
		data, err := json.Marshal(&lease)
		if err != nil {
			return err
		}
		if err := leases.Put([]byte(leaseID), data); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) BeginRelease(leaseID string) error {
	return s.updateLease(leaseID, func(lease *Lease) {
		lease.Releasing = true
		lease.Attempts++
		lease.LastError = ""
		lease.UpdatedAt = s.now().UTC().Unix()
	})
}

func (s *Store) RetainLease(leaseID, owner string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(leasesBucket)
		raw := bucket.Get([]byte(leaseID))
		if raw == nil {
			return nil
		}
		var lease Lease
		if err := json.Unmarshal(raw, &lease); err != nil {
			return err
		}
		if lease.Owner != owner {
			return fmt.Errorf("lease %q is owned by %q", leaseID, lease.Owner)
		}
		if !lease.Releasing {
			return nil
		}
		lease.Releasing = false
		lease.LastError = ""
		lease.UpdatedAt = s.now().UTC().Unix()
		data, err := json.Marshal(&lease)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(leaseID), data)
	})
}

func (s *Store) RecordReleaseFailure(leaseID string, releaseErr error) error {
	return s.updateLease(leaseID, func(lease *Lease) {
		lease.Releasing = true
		lease.LastError = releaseErr.Error()
		lease.UpdatedAt = s.now().UTC().Unix()
	})
}

func (s *Store) updateLease(leaseID string, update func(*Lease)) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(leasesBucket)
		raw := bucket.Get([]byte(leaseID))
		if raw == nil {
			return nil
		}
		var lease Lease
		if err := json.Unmarshal(raw, &lease); err != nil {
			return err
		}
		update(&lease)
		data, err := json.Marshal(&lease)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(leaseID), data)
	})
}

func (s *Store) GetMount(key string) (*Record, error) {
	var out *Record
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(mountsBucket).Get([]byte(key))
		if raw == nil {
			return nil
		}
		out = &Record{}
		return json.Unmarshal(raw, out)
	})
	return out, err
}

func (s *Store) GetLease(id string) (*Lease, error) {
	var out *Lease
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(leasesBucket).Get([]byte(id))
		if raw == nil {
			return nil
		}
		out = &Lease{}
		return json.Unmarshal(raw, out)
	})
	return out, err
}

func (s *Store) LeaseCount(mountKey string) (int, error) {
	count := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(leasesBucket).ForEach(func(_, raw []byte) error {
			var lease Lease
			if err := json.Unmarshal(raw, &lease); err != nil {
				return err
			}
			if lease.MountKey == mountKey {
				count++
			}
			return nil
		})
	})
	return count, err
}

// ReleaseLease removes a lease after the caller has completed any required
// resource unmount. removeMount must only be true for the final lease.
func (s *Store) ReleaseLease(leaseID string, removeMount bool) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		leases := tx.Bucket(leasesBucket)
		raw := leases.Get([]byte(leaseID))
		if raw == nil {
			return nil
		}
		var lease Lease
		if err := json.Unmarshal(raw, &lease); err != nil {
			return err
		}
		if removeMount {
			cursor := leases.Cursor()
			for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
				if string(key) == leaseID {
					continue
				}
				var other Lease
				if err := json.Unmarshal(value, &other); err != nil {
					return err
				}
				if other.MountKey == lease.MountKey {
					return fmt.Errorf("mount %q still has active leases", lease.MountKey)
				}
			}
			if err := tx.Bucket(mountsBucket).Delete([]byte(lease.MountKey)); err != nil {
				return err
			}
		}
		return leases.Delete([]byte(leaseID))
	})
}

func (s *Store) List() ([]Record, error) {
	records := make([]Record, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(mountsBucket).ForEach(func(_, raw []byte) error {
			var record Record
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			records = append(records, record)
			return nil
		})
	})
	sort.Slice(records, func(i, j int) bool { return records[i].ImageURL < records[j].ImageURL })
	return records, err
}

func (s *Store) ListLeases() ([]Lease, error) {
	leases := make([]Lease, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(leasesBucket).ForEach(func(_, raw []byte) error {
			var lease Lease
			if err := json.Unmarshal(raw, &lease); err != nil {
				return err
			}
			leases = append(leases, lease)
			return nil
		})
	})
	sort.Slice(leases, func(i, j int) bool { return leases[i].ID < leases[j].ID })
	return leases, err
}

func (s *Store) ListReleasing() ([]Lease, error) {
	leases, err := s.ListLeases()
	if err != nil {
		return nil, err
	}
	out := leases[:0]
	for _, lease := range leases {
		if lease.Releasing {
			out = append(out, lease)
		}
	}
	return out, nil
}

func (s *Store) Close() error { return s.db.Close() }
