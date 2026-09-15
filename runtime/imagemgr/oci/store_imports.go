package oci

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (s *metadataStore) putImport(ref *importedRefRecord, content *importedContentRecord) error {
	refData, err := json.Marshal(ref)
	if err != nil {
		return fmt.Errorf("marshal imported ref: %w", err)
	}
	contentData, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshal imported content: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(importContentsBucket).Put([]byte(content.ContentDigest), contentData); err != nil {
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
		content, err := getImportContentTx(tx, ref.ContentDigest)
		if err != nil || content == nil {
			if err == nil {
				err = fmt.Errorf("imported ref %s points to missing content %s", imageURL, ref.ContentDigest)
			}
			return err
		}
		record = joinImportedRecord(ref.ImageURL, content)
		return nil
	})
	return record, err
}

func (s *metadataStore) getImportContent(digest string) (*importedContentRecord, error) {
	var record *importedContentRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		record, err = getImportContentTx(tx, digest)
		return err
	})
	return record, err
}

func getImportContentTx(tx *bolt.Tx, digest string) (*importedContentRecord, error) {
	v := tx.Bucket(importContentsBucket).Get([]byte(digest))
	if v == nil {
		return nil, nil
	}
	var record importedContentRecord
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
			content, err := getImportContentTx(tx, ref.ContentDigest)
			if err != nil {
				return err
			}
			if content == nil {
				return fmt.Errorf("imported ref %s points to missing content %s", ref.ImageURL, ref.ContentDigest)
			}
			records = append(records, joinImportedRecord(ref.ImageURL, content))
			return nil
		})
	})
	return records, err
}

func joinImportedRecord(imageURL string, content *importedContentRecord) *ImportedImageRecord {
	return &ImportedImageRecord{
		ImageURL: imageURL, ContentDigest: content.ContentDigest,
		ArchivePath: content.ArchivePath, ArchiveDigest: content.ArchiveDigest,
		PlatformOS: content.PlatformOS, PlatformArch: content.PlatformArch,
		PlatformVariant: content.PlatformVariant, SizeBytes: content.SizeBytes,
		ImportedAtUnix: content.ImportedAtUnix,
	}
}

func (s *metadataStore) listImportContents() ([]*importedContentRecord, error) {
	records := make([]*importedContentRecord, 0, 16)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importContentsBucket).ForEach(func(_, value []byte) error {
			var record importedContentRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			records = append(records, &record)
			return nil
		})
	})
	return records, err
}

func (s *metadataStore) importContentReferenced(digest string) (bool, error) {
	referenced := false
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(importRefsBucket).ForEach(func(_, value []byte) error {
			var record importedRefRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			if record.ContentDigest == digest {
				referenced = true
			}
			return nil
		})
	})
	return referenced, err
}

func (s *metadataStore) deleteImportContent(digest string) error {
	return s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(importContentsBucket).Delete([]byte(digest)) })
}
