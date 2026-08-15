package oci

import (
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

var (
	layerRecordsBucket      = []byte("layer_records")
	mountRecordsBucket      = []byte("mount_records")
	mountTxnBucket          = []byte("mount_txn_records")
	layerDirMapBucket       = []byte("layer_dir_map")
	chainRecordsBucket      = []byte("chain_records")
	chainDirMapBucket       = []byte("chain_dir_map")
	importRefsBucket        = []byte("import_refs")
	importGenerationsBucket = []byte("import_generations")

	ErrLayerNotFound = errors.New("layer not found")
	ErrChainNotFound = errors.New("chain not found")
)

// LayerRecord stores local extracted layer metadata.
type LayerRecord struct {
	Digest        string `json:"digest"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
	RefCount      int    `json:"ref_count"`
	RefZeroAtUnix int64  `json:"ref_zero_at_unix"`
	LastUsedUnix  int64  `json:"last_used_unix"`
}

// ChainRecord stores local lowdir metadata keyed by Docker-style chainID.
type ChainRecord struct {
	ChainID       string `json:"chain_id"`
	Path          string `json:"path"`
	RefCount      int    `json:"ref_count"`
	RefZeroAtUnix int64  `json:"ref_zero_at_unix"`
	LastUsedUnix  int64  `json:"last_used_unix"`
}

// OciMountRecord stores mounted OCI image metadata.
type OciMountRecord struct {
	CacheKey      string       `json:"cache_key,omitempty"`
	ImageURL      string       `json:"image_url"`
	MountID       string       `json:"mount_id"`
	MountPath     string       `json:"mount_path"`
	LayerDigests  []string     `json:"layer_digests"`
	ChainIDs      []string     `json:"chain_ids,omitempty"`
	LowerDirs     []string     `json:"lower_dirs"`
	Env           []string     `json:"env,omitempty"`
	ImageConfig   *ImageConfig `json:"image_config,omitempty"`
	CreatedAtUnix int64        `json:"created_at_unix"`
}

// OciMountTxnRecord stores in-progress mount transaction metadata.
type OciMountTxnRecord struct {
	CacheKey      string   `json:"cache_key,omitempty"`
	ImageURL      string   `json:"image_url"`
	MountID       string   `json:"mount_id"`
	MountPath     string   `json:"mount_path"`
	LayerDigests  []string `json:"layer_digests"`
	ChainIDs      []string `json:"chain_ids,omitempty"`
	LowerDirs     []string `json:"lower_dirs"`
	CreatedAtUnix int64    `json:"created_at_unix"`
}

// ImportedImageRecord is the current immutable generation selected by a mutable ref.
type ImportedImageRecord struct {
	ImageURL         string `json:"image_url"`
	GenerationDigest string `json:"generation_digest"`
	ArchivePath      string `json:"archive_path"`
	ArchiveDigest    string `json:"archive_digest"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	PlatformVariant  string `json:"platform_variant,omitempty"`
	SizeBytes        int64  `json:"size_bytes"`
	ImportedAtUnix   int64  `json:"imported_at_unix"`
}

type importedRefRecord struct {
	ImageURL         string `json:"image_url"`
	GenerationDigest string `json:"generation_digest"`
}

type importedGenerationRecord struct {
	GenerationDigest string `json:"generation_digest"`
	ArchivePath      string `json:"archive_path"`
	ArchiveDigest    string `json:"archive_digest"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	PlatformVariant  string `json:"platform_variant,omitempty"`
	SizeBytes        int64  `json:"size_bytes"`
	ImportedAtUnix   int64  `json:"imported_at_unix"`
}

type metadataStore struct {
	db *bolt.DB
}

func openMetadataStore(dbPath string) (*metadataStore, error) {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open oci metadata db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(layerRecordsBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(layerDirMapBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(chainRecordsBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(chainDirMapBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(mountRecordsBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(mountTxnBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(importRefsBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(importGenerationsBucket); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init oci metadata db buckets: %w", err)
	}

	return &metadataStore{db: db}, nil
}

func (s *metadataStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
