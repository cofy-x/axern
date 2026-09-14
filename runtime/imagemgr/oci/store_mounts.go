package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putMount(record *OciMountState) error {
	key := record.mountKey()
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal mount record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(ociMountStateBucket).Put([]byte(key), data)
	})
}

func (s *metadataStore) getMount(cacheKey string) (*OciMountState, error) {
	var record *OciMountState
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(ociMountStateBucket).Get([]byte(cacheKey))
		if v == nil {
			return nil
		}
		record = &OciMountState{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) deleteMount(cacheKey string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(ociMountStateBucket).Delete([]byte(cacheKey))
	})
}

func (s *metadataStore) listMounts() ([]*OciMountState, error) {
	records := make([]*OciMountState, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(ociMountStateBucket).ForEach(func(_, v []byte) error {
			r := &OciMountState{}
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

func (r *OciMountState) mountKey() string {
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
