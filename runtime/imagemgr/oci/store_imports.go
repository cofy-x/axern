package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putImport(record *ImportedImageRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal import record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(importRecordsBucket).Put([]byte(record.ImageURL), data)
	})
}

func (s *metadataStore) getImport(imageURL string) (*ImportedImageRecord, error) {
	var record *ImportedImageRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(importRecordsBucket).Get([]byte(imageURL))
		if v == nil {
			return nil
		}
		record = &ImportedImageRecord{}
		return json.Unmarshal(v, record)
	})
	return record, err
}

func (s *metadataStore) listImports() ([]*ImportedImageRecord, error) {
	records := make([]*ImportedImageRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importRecordsBucket).ForEach(func(_, v []byte) error {
			r := &ImportedImageRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return err
			}
			records = append(records, r)
			return nil
		})
	})
	return records, err
}
