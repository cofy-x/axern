package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putImport(ref *importedRefRecord, generation *importedGenerationRecord) error {
	refData, err := json.Marshal(ref)
	if err != nil {
		return fmt.Errorf("marshal imported ref: %w", err)
	}
	generationData, err := json.Marshal(generation)
	if err != nil {
		return fmt.Errorf("marshal imported generation: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(importGenerationsBucket).Put([]byte(generation.GenerationDigest), generationData); err != nil {
			return err
		}
		return tx.Bucket(importRefsBucket).Put([]byte(ref.ImageURL), refData)
	})
}

func (s *metadataStore) getImport(imageURL string) (*ImportedImageRecord, error) {
	var record *ImportedImageRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(importRefsBucket).Get([]byte(imageURL))
		if v == nil {
			return nil
		}
		var ref importedRefRecord
		if err := json.Unmarshal(v, &ref); err != nil {
			return err
		}
		generation, err := getImportGenerationTx(tx, ref.GenerationDigest)
		if err != nil || generation == nil {
			if err == nil {
				err = fmt.Errorf("imported ref %s points to missing generation %s", imageURL, ref.GenerationDigest)
			}
			return err
		}
		record = joinImportedRecord(ref.ImageURL, generation)
		return nil
	})
	return record, err
}

func (s *metadataStore) getImportGeneration(digest string) (*importedGenerationRecord, error) {
	var record *importedGenerationRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		record, err = getImportGenerationTx(tx, digest)
		return err
	})
	return record, err
}

func getImportGenerationTx(tx *bolt.Tx, digest string) (*importedGenerationRecord, error) {
	v := tx.Bucket(importGenerationsBucket).Get([]byte(digest))
	if v == nil {
		return nil, nil
	}
	var record importedGenerationRecord
	if err := json.Unmarshal(v, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *metadataStore) listImports() ([]*ImportedImageRecord, error) {
	records := make([]*ImportedImageRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importRefsBucket).ForEach(func(_, v []byte) error {
			var ref importedRefRecord
			if err := json.Unmarshal(v, &ref); err != nil {
				return err
			}
			generation, err := getImportGenerationTx(tx, ref.GenerationDigest)
			if err != nil {
				return err
			}
			if generation == nil {
				return fmt.Errorf("imported ref %s points to missing generation %s", ref.ImageURL, ref.GenerationDigest)
			}
			records = append(records, joinImportedRecord(ref.ImageURL, generation))
			return nil
		})
	})
	return records, err
}

func joinImportedRecord(imageURL string, generation *importedGenerationRecord) *ImportedImageRecord {
	return &ImportedImageRecord{
		ImageURL: imageURL, GenerationDigest: generation.GenerationDigest,
		ArchivePath: generation.ArchivePath, ArchiveDigest: generation.ArchiveDigest,
		PlatformOS: generation.PlatformOS, PlatformArch: generation.PlatformArch,
		PlatformVariant: generation.PlatformVariant, SizeBytes: generation.SizeBytes,
		ImportedAtUnix: generation.ImportedAtUnix,
	}
}

func (s *metadataStore) listImportGenerations() ([]*importedGenerationRecord, error) {
	records := make([]*importedGenerationRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importGenerationsBucket).ForEach(func(_, value []byte) error {
			var record importedGenerationRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			records = append(records, &record)
			return nil
		})
	})
	return records, err
}

func (s *metadataStore) importGenerationReferenced(digest string) (bool, error) {
	referenced := false
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importRefsBucket).ForEach(func(_, value []byte) error {
			var record importedRefRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			if record.GenerationDigest == digest {
				referenced = true
			}
			return nil
		})
	})
	return referenced, err
}

func (s *metadataStore) deleteImportGeneration(digest string) error {
	return s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(importGenerationsBucket).Delete([]byte(digest)) })
}
