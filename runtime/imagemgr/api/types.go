package api

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
)

type OSSMountRequest struct {
	MountPoint      string `json:"mount_point,omitempty"`
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	Object          string `json:"object"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	AccessKeySecret string `json:"access_key_secret,omitempty"`
	LeaseID         string `json:"lease_id"`
	Owner           string `json:"owner,omitempty"`
}

func (req *OSSMountRequest) String() string {
	return fmt.Sprintf("(%s, %s, %s, %s)", req.MountPoint, req.Endpoint, req.Bucket, req.Object)
}

type OSSUmountRequest struct {
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Object   string `json:"object"`
	LeaseID  string `json:"lease_id"`
}

func (req *OSSUmountRequest) String() string {
	return fmt.Sprintf("(%s %s %s)", req.Endpoint, req.Bucket, req.Object)
}

type MountInfo struct {
	MountPath      string          `json:"mount_path"`
	MountPoint     string          `json:"mount_point,omitempty"`
	Env            []string        `json:"env,omitempty"`
	ImageConfig    *ImageConfig    `json:"image_config,omitempty"`
	ImmutableMount *ImmutableMount `json:"immutable_mount,omitempty"`
}

// ImmutableMount is the source-owned effective lower contract returned with
// every mounted rootfs lease. Runtime projection consumes this descriptor and
// never reverse-engineers OCI, Nydus, OSS, or future EROFS implementations.
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
	MountTypeOCI   MountType = "oci"
	MountTypeNydus MountType = "nydus"
	MountTypeOSS   MountType = "oss"
)

// MountedImageDetail is returned by /list_oci_mount_details for both OCI and
// Nydus-backed image mounts.
type MountedImageDetail struct {
	ImageURL      string    `json:"image_url"`
	CacheKey      string    `json:"cache_key,omitempty"`
	MountType     MountType `json:"mount_type"`
	NydusImageURL string    `json:"nydus_image_url,omitempty"`
	MountPath     string    `json:"mount_path"`
	LeaseCount    int       `json:"lease_count"`
}

type ImageLocalityEntry struct {
	Key                        string    `json:"key"`
	MountType                  MountType `json:"mount_type"`
	ImageURL                   string    `json:"image_url,omitempty"`
	NydusImageURL              string    `json:"nydus_image_url,omitempty"`
	MountPath                  string    `json:"mount_path,omitempty"`
	Mounted                    bool      `json:"mounted"`
	DaemonID                   string    `json:"daemon_id,omitempty"`
	DaemonAlive                bool      `json:"daemon_alive"`
	ChunkDBTotalChunks         int64     `json:"chunkdb_total_chunks"`
	ChunkDBUsedBytes           int64     `json:"chunkdb_used_bytes"`
	ChunkDBRecentAccessAgeSecs int64     `json:"chunkdb_recent_access_age_secs"`
	PeerHealthyCount           int64     `json:"peer_healthy_count"`
	PeerUnhealthyCount         int64     `json:"peer_unhealthy_count"`
	PeerHintedCount            int64     `json:"peer_hinted_count"`
}

type InventoryResponse struct {
	MountedImages  []MountedImageDetail   `json:"mounted_images"`
	ImportedImages []ImportedImageDetail  `json:"imported_images,omitempty"`
	Locality       []ImageLocalityEntry   `json:"locality"`
	Daemons        []imagefsd.DaemonInfo  `json:"daemons"`
	ChunkDB        *imagefsd.ChunkDBStats `json:"chunkdb,omitempty"`
	ChunkDBError   string                 `json:"chunkdb_error,omitempty"`
	LocalityError  string                 `json:"locality_error,omitempty"`
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
	SourceRef        string `json:"source_ref"`
	CanonicalRef     string `json:"canonical_ref"`
	ImmutableRef     string `json:"immutable_ref"`
	GenerationDigest string `json:"generation_digest"`
	ArchiveDigest    string `json:"archive_digest"`
	Platform         string `json:"platform"`
	SizeBytes        int64  `json:"size_bytes"`
	Reused           bool   `json:"reused"`
}

type OCIResolveResponse struct {
	CanonicalRef string `json:"canonical_ref"`
	CacheKey     string `json:"cache_key"`
	Imported     bool   `json:"imported"`
}

type ImportedImageDetail struct {
	ImageRef         string `json:"image_ref"`
	GenerationDigest string `json:"generation_digest"`
	ArchiveDigest    string `json:"archive_digest"`
	Platform         string `json:"platform"`
	SizeBytes        int64  `json:"size_bytes"`
	ImportedAtUnix   int64  `json:"imported_at_unix"`
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

// NydusMountRequest is used to request mounting a Nydus image
type NydusMountRequest struct {
	ImageURL         string `json:"image_url"`
	MountPoint       string `json:"mount_point,omitempty"`
	DockerConfigJSON string `json:"docker_config_json,omitempty"`
	LeaseID          string `json:"lease_id"`
	Owner            string `json:"owner,omitempty"`
}

func (req *NydusMountRequest) String() string {
	return fmt.Sprintf("(%s, %s)", req.ImageURL, req.MountPoint)
}

// NydusUmountRequest is used to request unmounting a Nydus image
type NydusUmountRequest struct {
	ImageURL string `json:"image_url"`
	LeaseID  string `json:"lease_id"`
}

func (req *NydusUmountRequest) String() string {
	return fmt.Sprintf("(%s)", req.ImageURL)
}

// CleanupDaemonRequest is used to request cleanup of a daemon
type CleanupDaemonRequest struct {
	DaemonID string `json:"daemon_id"`
}
