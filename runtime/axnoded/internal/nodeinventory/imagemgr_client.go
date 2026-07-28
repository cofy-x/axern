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
	ImageURL      string `json:"image_url"`
	MountType     string `json:"mount_type"`
	NydusImageURL string `json:"nydus_image_url,omitempty"`
	MountPath     string `json:"mount_path"`
}

type ImportedImageDetail struct {
	ImageRef       string `json:"image_ref"`
	ArchivePath    string `json:"archive_path"`
	SizeBytes      int64  `json:"size_bytes"`
	ImportedAtUnix int64  `json:"imported_at_unix"`
}

type DaemonInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MountPoint   string `json:"mount_point"`
	SourceType   string `json:"source_type"`
	IsAlive      bool   `json:"is_alive"`
	ImageURL     string `json:"image_url,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	ObjectPrefix string `json:"object_prefix,omitempty"`
}

type ChunkDBStats struct {
	Chunks struct {
		TotalCount int64 `json:"total_count"`
	} `json:"chunks"`
	Storage struct {
		FreeSizeBytes  int64  `json:"free_size_bytes"`
		TotalSizeBytes int64  `json:"total_size_bytes"`
		UsagePercent   string `json:"usage_percent"`
		UsedSizeBytes  int64  `json:"used_size_bytes"`
	} `json:"storage"`
}

type ImageLocalityEntry struct {
	Key                        string `json:"key"`
	MountType                  string `json:"mount_type"`
	ImageURL                   string `json:"image_url,omitempty"`
	NydusImageURL              string `json:"nydus_image_url,omitempty"`
	MountPath                  string `json:"mount_path,omitempty"`
	Mounted                    bool   `json:"mounted"`
	DaemonID                   string `json:"daemon_id,omitempty"`
	DaemonAlive                bool   `json:"daemon_alive"`
	ChunkDBTotalChunks         int64  `json:"chunkdb_total_chunks"`
	ChunkDBUsedBytes           int64  `json:"chunkdb_used_bytes"`
	ChunkDBRecentAccessAgeSecs int64  `json:"chunkdb_recent_access_age_secs"`
	PeerHealthyCount           int64  `json:"peer_healthy_count"`
	PeerUnhealthyCount         int64  `json:"peer_unhealthy_count"`
	PeerHintedCount            int64  `json:"peer_hinted_count"`
}

type ImageManagerInventory struct {
	MountedImages  []MountedImageDetail  `json:"mounted_images"`
	ImportedImages []ImportedImageDetail `json:"imported_images,omitempty"`
	Locality       []ImageLocalityEntry  `json:"locality"`
	Daemons        []DaemonInfo          `json:"daemons"`
	ChunkDB        *ChunkDBStats         `json:"chunkdb,omitempty"`
	ChunkDBError   string                `json:"chunkdb_error,omitempty"`
	LocalityError  string                `json:"locality_error,omitempty"`
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
	return &inventory, nil
}
