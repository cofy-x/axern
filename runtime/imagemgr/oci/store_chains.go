package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putChain(record *ChainRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal chain record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(chainRecordsBucket).Put([]byte(record.ChainID), data)
	})
}

func (s *metadataStore) getChain(chainID string) (*ChainRecord, error) {
	var record *ChainRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(chainRecordsBucket).Get([]byte(chainID))
		if v == nil {
			return nil
		}
		record = &ChainRecord{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) getOrCreateChainDir(chainID string) (string, error) {
	var dir string
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(chainDirMapBucket)
		if v := b.Get([]byte(chainID)); v != nil {
			dir = string(v)
			return nil
		}

		seq, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("failed to allocate chain dir sequence: %w", err)
		}
		dir = fmt.Sprintf("c%d", seq)
		return b.Put([]byte(chainID), []byte(dir))
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (s *metadataStore) incrementChainRef(chainID string, lastUsedUnix int64) (*ChainRecord, error) {
	var updated *ChainRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(chainRecordsBucket)
		v := b.Get([]byte(chainID))
		if v == nil {
			return ErrChainNotFound
		}
		rec := &ChainRecord{}
		if err := json.Unmarshal(v, rec); err != nil {
			return err
		}
		rec.RefCount++
		rec.RefZeroAtUnix = 0
		rec.LastUsedUnix = lastUsedUnix
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(chainID), data); err != nil {
			return err
		}
		updated = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *metadataStore) decrementChainRef(chainID string, lastUsedUnix int64) (*ChainRecord, error) {
	var updated *ChainRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(chainRecordsBucket)
		v := b.Get([]byte(chainID))
		if v == nil {
			return ErrChainNotFound
		}
		rec := &ChainRecord{}
		if err := json.Unmarshal(v, rec); err != nil {
			return err
		}
		if rec.RefCount > 0 {
			rec.RefCount--
			if rec.RefCount == 0 {
				rec.RefZeroAtUnix = lastUsedUnix
			}
		} else if rec.RefZeroAtUnix == 0 {
			rec.RefZeroAtUnix = lastUsedUnix
		}
		rec.LastUsedUnix = lastUsedUnix
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(chainID), data); err != nil {
			return err
		}
		updated = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *metadataStore) deleteChain(chainID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(chainRecordsBucket).Delete([]byte(chainID))
	})
}

func (s *metadataStore) listChains() ([]*ChainRecord, error) {
	records := make([]*ChainRecord, 0, 32)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(chainRecordsBucket).ForEach(func(_, v []byte) error {
			r := &ChainRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return err
			}
			records = append(records, r)
			return nil
		})
	})
	return records, err
}
