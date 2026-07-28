package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putMount(record *OciMountRecord) error {
	key := record.mountKey()
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal mount record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mountRecordsBucket).Put([]byte(key), data)
	})
}

func (s *metadataStore) getMount(cacheKey string) (*OciMountRecord, error) {
	var record *OciMountRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(mountRecordsBucket).Get([]byte(cacheKey))
		if v == nil {
			return nil
		}
		record = &OciMountRecord{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) deleteMount(cacheKey string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mountRecordsBucket).Delete([]byte(cacheKey))
	})
}

func (s *metadataStore) listMounts() ([]*OciMountRecord, error) {
	records := make([]*OciMountRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(mountRecordsBucket).ForEach(func(_, v []byte) error {
			r := &OciMountRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return err
			}
			records = append(records, r)
			return nil
		})
	})
	return records, err
}

func (s *metadataStore) putMountTxn(record *OciMountTxnRecord) error {
	key := record.mountKey()
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal mount txn record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mountTxnBucket).Put([]byte(key), data)
	})
}

func (s *metadataStore) getMountTxn(cacheKey string) (*OciMountTxnRecord, error) {
	var record *OciMountTxnRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(mountTxnBucket).Get([]byte(cacheKey))
		if v == nil {
			return nil
		}
		record = &OciMountTxnRecord{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) deleteMountTxn(cacheKey string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mountTxnBucket).Delete([]byte(cacheKey))
	})
}

func (s *metadataStore) listMountTxns() ([]*OciMountTxnRecord, error) {
	records := make([]*OciMountTxnRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(mountTxnBucket).ForEach(func(_, v []byte) error {
			r := &OciMountTxnRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return err
			}
			records = append(records, r)
			return nil
		})
	})
	return records, err
}

func (r *OciMountRecord) mountKey() string {
	if r != nil && r.CacheKey != "" {
		return r.CacheKey
	}
	if r == nil {
		return ""
	}
	return r.ImageURL
}

func (r *OciMountTxnRecord) mountKey() string {
	if r != nil && r.CacheKey != "" {
		return r.CacheKey
	}
	if r == nil {
		return ""
	}
	return r.ImageURL
}
