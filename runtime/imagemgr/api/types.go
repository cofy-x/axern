package api

import (
	"fmt"
)

// ImmutableMount is the source-owned effective lower contract returned with
// every mounted rootfs lease. Runtime projection consumes this descriptor and
// never reverse-engineers backend filesystem details.
type ImmutableMount struct {
	Identity           string   `json:"identity"`
	EffectiveRoot      string   `json:"effective_root"`
	Filesystem         string   `json:"filesystem"`
	BackingFilesystems []string `json:"backing_filesystems,omitempty"`
	LowerDirs          []string `json:"lower_dirs"`
	Readonly           bool     `json:"readonly"`
	LeaseID            string   `json:"lease_id"`
}

type ImageConfig struct {
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
	User       string   `json:"user,omitempty"`
}

type MountType string

const (
	MountTypeOCI MountType = "oci"
)

// MountedImageDetail is returned by /list_oci_mount_details.
type MountedImageDetail struct {
	ImageURL   string    `json:"image_url"`
	CacheKey   string    `json:"cache_key,omitempty"`
	MountType  MountType `json:"mount_type"`
	MountPath  string    `json:"mount_path"`
	LeaseCount int       `json:"lease_count"`
}

type InventoryResponse struct {
	MountedImages  []MountedImageDetail  `json:"mounted_images"`
	ImportedImages []ImportedImageDetail `json:"imported_images,omitempty"`
}

type ReconcileMountLeasesRequest struct {
	Owner    string   `json:"owner"`
	LeaseIDs []string `json:"lease_ids"`
}

type ReconcileMountLeasesResponse struct {
	Retained  int `json:"retained"`
	Releasing int `json:"releasing"`
}

type OCIImportResponse struct {
	SourceRef     string `json:"source_ref"`
	CanonicalRef  string `json:"canonical_ref"`
	ImmutableRef  string `json:"immutable_ref"`
	ContentDigest string `json:"content_digest"`
	ArchiveDigest string `json:"archive_digest"`
	Platform      string `json:"platform"`
	SizeBytes     int64  `json:"size_bytes"`
	Reused        bool   `json:"reused"`
}

type OCIResolveResponse struct {
	CanonicalRef string `json:"canonical_ref"`
	CacheKey     string `json:"cache_key"`
	Imported     bool   `json:"imported"`
}

type ImportedImageDetail struct {
	CacheKey       string `json:"cache_key"`
	ImageRef       string `json:"image_ref"`
	ContentDigest  string `json:"content_digest"`
	ArchiveDigest  string `json:"archive_digest"`
	Platform       string `json:"platform"`
	SizeBytes      int64  `json:"size_bytes"`
	ImportedAtUnix int64  `json:"imported_at_unix"`
}

// OCIMountResponse is returned by the /oci_mount endpoint.
type OCIMountResponse struct {
	MountPath      string          `json:"mount_path"`
	Env            []string        `json:"env,omitempty"`
	ImageConfig    *ImageConfig    `json:"image_config,omitempty"`
	ImmutableMount *ImmutableMount `json:"immutable_mount"`
}

// OCIMountRequest is used to request mounting an OCI image
type OCIMountRequest struct {
	ImageURL         string `json:"image_url"`                    // Image URL, e.g., "library/alpine:latest"
	CacheKey         string `json:"cache_key,omitempty"`          // Optional caller-resolved cache identity.
	DockerConfigJSON string `json:"docker_config_json,omitempty"` // Optional request-scoped registry auth.
	LeaseID          string `json:"lease_id"`
	Owner            string `json:"owner,omitempty"`
}

func (req *OCIMountRequest) String() string {
	return fmt.Sprintf("(%s)", req.ImageURL)
}

// OCIUmountRequest is used to request unmounting an OCI image
type OCIUmountRequest struct {
	ImageURL string `json:"image_url"` // Image URL
	CacheKey string `json:"cache_key,omitempty"`
	LeaseID  string `json:"lease_id"`
}

func (req *OCIUmountRequest) String() string {
	return fmt.Sprintf("(%s)", req.ImageURL)
}
