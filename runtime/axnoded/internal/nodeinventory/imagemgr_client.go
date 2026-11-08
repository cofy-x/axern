package nodeinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

const imageManagerInventoryTimeout = 3 * time.Second

type MountedImageDetail struct {
	CacheKey   string `json:"cache_key,omitempty"`
	LeaseCount int    `json:"lease_count"`
	ImageURL   string `json:"image_url"`
	MountType  string `json:"mount_type"`
	MountPath  string `json:"mount_path"`
}

type ImportedImageDetail struct {
	CacheKey       string `json:"cache_key,omitempty"`
	ImageRef       string `json:"image_ref"`
	ContentDigest  string `json:"content_digest"`
	ArchiveDigest  string `json:"archive_digest"`
	Platform       string `json:"platform"`
	SizeBytes      int64  `json:"size_bytes"`
	ImportedAtUnix int64  `json:"imported_at_unix"`
}

type ImageManagerInventory struct {
	MountedImages  []MountedImageDetail  `json:"mounted_images"`
	ImportedImages []ImportedImageDetail `json:"imported_images,omitempty"`
}

type ImageManagerClient struct {
	client  *http.Client
	enabled bool
}

func NewImageManagerClient(enabled bool, sockPath string) *ImageManagerClient {
	if !enabled {
		return &ImageManagerClient{}
	}
	if sockPath == "" {
		sockPath = config.DefaultImageManagerSocket
	}
	return &ImageManagerClient{
		enabled: true,
		client: &http.Client{
			Timeout: imageManagerInventoryTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					dialer := net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}
					return dialer.DialContext(ctx, "unix", sockPath)
				},
			},
		},
	}
}

func (c *ImageManagerClient) Enabled() bool {
	return c != nil && c.enabled && c.client != nil
}

func (c *ImageManagerClient) Inventory(ctx context.Context) (*ImageManagerInventory, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("imagemgr inventory disabled")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/inventory", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request imagemgr inventory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imagemgr inventory status %d", resp.StatusCode)
	}
	var inventory ImageManagerInventory
	if err := json.NewDecoder(resp.Body).Decode(&inventory); err != nil {
		return nil, fmt.Errorf("decode imagemgr inventory: %w", err)
	}
	for _, mounted := range inventory.MountedImages {
		if mounted.MountType != "oci" || mounted.ImageURL == "" || mounted.MountPath == "" {
			return nil, fmt.Errorf("imagemgr returned an unsupported or incomplete mounted image")
		}
	}
	return &inventory, nil
}
