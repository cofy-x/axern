package langruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

type imageManagerMountInfo struct {
	MountPath      string                      `json:"mount_path"`
	Env            []string                    `json:"env,omitempty"`
	ImageConfig    *ImageConfig                `json:"image_config,omitempty"`
	ImmutableMount *imageManagerImmutableMount `json:"immutable_mount"`
}

type imageManagerImmutableMount struct {
	Identity           string   `json:"identity"`
	EffectiveRoot      string   `json:"effective_root"`
	Filesystem         string   `json:"filesystem"`
	BackingFilesystems []string `json:"backing_filesystems,omitempty"`
	LowerDirs          []string `json:"lower_dirs"`
	Readonly           bool     `json:"readonly"`
	LeaseID            string   `json:"lease_id"`
}

type ociMountRequest struct {
	ImageURL         string `json:"image_url"`
	CacheKey         string `json:"cache_key,omitempty"`
	DockerConfigJSON string `json:"docker_config_json,omitempty"`
	LeaseID          string `json:"lease_id"`
	Owner            string `json:"owner,omitempty"`
}

type ociUmountRequest struct {
	ImageURL string `json:"image_url"`
	CacheKey string `json:"cache_key,omitempty"`
	LeaseID  string `json:"lease_id"`
}

type imageManagerInventory struct {
	ImportedImages []imageManagerImportedImage `json:"imported_images,omitempty"`
}

type imageManagerImportedImage struct {
	ImageRef       string `json:"image_ref"`
	ArchiveDigest  string `json:"archive_digest,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	ImportedAtUnix int64  `json:"imported_at_unix,omitempty"`
}

type ossMountRequest struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	Object          string `json:"object"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	AccessKeySecret string `json:"access_key_secret,omitempty"`
	LeaseID         string `json:"lease_id"`
	Owner           string `json:"owner,omitempty"`
}

type ossUmountRequest struct {
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Object   string `json:"object"`
	LeaseID  string `json:"lease_id"`
}

type reconcileMountLeasesRequest struct {
	Owner    string   `json:"owner"`
	LeaseIDs []string `json:"lease_ids"`
}

type httpImageManagerClient struct {
	clt *http.Client
}

func newImageManagerClient(enabled bool, sockPath string) imageManagerClient {
	if !enabled {
		return nil
	}
	if sockPath == "" {
		sockPath = config.DefaultImageManagerSocket
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}
				return dialer.DialContext(ctx, "unix", sockPath)
			},
		},
	}
	return &httpImageManagerClient{clt: client}
}

func (c *httpImageManagerClient) MountOCI(req *ociMountRequest) (*imageManagerMountInfo, error) {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oci_mount", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to mount oci image %s: %w", req.ImageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to mount oci image %s: %s", req.ImageURL, string(errMsg))
	}
	result := &imageManagerMountInfo{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, fmt.Errorf("invalid oci mount response: %w", err)
	}
	if result.MountPath == "" {
		return nil, fmt.Errorf("mount_path not found in oci mount response")
	}
	return result, nil
}

func (c *httpImageManagerClient) ResolveOCIImageCacheKey(imageURL string) (string, error) {
	resp, err := c.clt.Get("http://unix/inventory")
	if err != nil {
		return "", fmt.Errorf("failed to query image inventory for %s: %w", imageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to query image inventory for %s: %s", imageURL, string(errMsg))
	}
	inventory := &imageManagerInventory{}
	if err := json.NewDecoder(resp.Body).Decode(inventory); err != nil {
		return "", fmt.Errorf("invalid image inventory response: %w", err)
	}
	for _, imported := range inventory.ImportedImages {
		if imported.ImageRef != imageURL {
			continue
		}
		if imported.ArchiveDigest != "" {
			return imageURL + "@" + imported.ArchiveDigest, nil
		}
		return "", fmt.Errorf("imported image %s has no archive digest; re-import the image", imageURL)
	}
	return imageURL, nil
}

func (c *httpImageManagerClient) UmountOCI(req *ociUmountRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oci_umount", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to unmount oci image %s: %w", req.ImageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to unmount oci image %s: %s", req.ImageURL, string(errMsg))
	}
	return nil
}

func (c *httpImageManagerClient) MountOSS(req *ossMountRequest) (*imageManagerMountInfo, error) {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oss_mount", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to mount oss rootfs %s/%s: %w", req.Bucket, req.Object, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to mount oss rootfs %s/%s: %s", req.Bucket, req.Object, string(errMsg))
	}
	result := &imageManagerMountInfo{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, fmt.Errorf("invalid oss mount response: %w", err)
	}
	if result.MountPath == "" {
		return nil, fmt.Errorf("mount_path not found in oss mount response")
	}
	return result, nil
}

func (c *httpImageManagerClient) UmountOSS(req *ossUmountRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oss_umount", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to unmount oss rootfs %s/%s: %w", req.Bucket, req.Object, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to unmount oss rootfs %s/%s: %s", req.Bucket, req.Object, string(errMsg))
	}
	return nil
}

func (c *httpImageManagerClient) ReconcileMountLeases(req *reconcileMountLeasesRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/reconcile_mount_leases", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to reconcile image mount leases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reconcile image mount leases: %s", string(errMsg))
	}
	return nil
}
