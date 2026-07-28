package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putLayer(record *LayerRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal layer record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(layerRecordsBucket).Put([]byte(record.Digest), data)
	})
}

func (s *metadataStore) getLayer(digest string) (*LayerRecord, error) {
	var record *LayerRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(layerRecordsBucket).Get([]byte(digest))
		if v == nil {
			return nil
		}
		record = &LayerRecord{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) getLayerDir(digest string) (string, error) {
	var dir string
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(layerDirMapBucket).Get([]byte(digest))
		if v == nil {
			return nil
		}
		dir = string(v)
		return nil
	})
	return dir, err
}

func (s *metadataStore) getOrCreateLayerDir(digest string) (string, error) {
	var dir string
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(layerDirMapBucket)
		if v := b.Get([]byte(digest)); v != nil {
			dir = string(v)
			return nil
		}

		seq, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("failed to allocate layer dir sequence: %w", err)
		}
		dir = fmt.Sprintf("l%d", seq)
		return b.Put([]byte(digest), []byte(dir))
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (s *metadataStore) incrementLayerRef(digest string, lastUsedUnix int64) (*LayerRecord, error) {
	var updated *LayerRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(layerRecordsBucket)
		v := b.Get([]byte(digest))
		if v == nil {
			return ErrLayerNotFound
		}
		rec := &LayerRecord{}
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
		if err := b.Put([]byte(digest), data); err != nil {
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

func (s *metadataStore) decrementLayerRef(digest string, lastUsedUnix int64) (*LayerRecord, error) {
	var updated *LayerRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(layerRecordsBucket)
		v := b.Get([]byte(digest))
		if v == nil {
			return ErrLayerNotFound
		}
		rec := &LayerRecord{}
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
		if err := b.Put([]byte(digest), data); err != nil {
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

func (s *metadataStore) deleteLayer(digest string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(layerRecordsBucket).Delete([]byte(digest))
	})
}

func (s *metadataStore) listLayers() ([]*LayerRecord, error) {
	records := make([]*LayerRecord, 0, 32)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(layerRecordsBucket).ForEach(func(_, v []byte) error {
			r := &LayerRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return err
			}
			records = append(records, r)
			return nil
		})
	})
	return records, err
}
