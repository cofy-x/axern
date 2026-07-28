package imagefsd

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/cgroup"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
)

type DaemonCreateOpt struct {
	ID         string
	Name       string
	MountPoint string
	// OSS Object = ObjectPrefix + Name
	ObjectPrefix    string
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	// Source type: "oss" or "nydus"
	SourceType       string
	RegistryAuth     string // For Nydus: base64 credentials for the selected repository.
	DockerConfigJSON string // Request-scoped auth used only while fetching the bootstrap.
	ImageURL         string // For Nydus: image URL to fetch bootstrap from registry
}

func (opts *DaemonCreateOpt) overwriteOSSConfig() bool {
	return opts.Endpoint != "" && opts.Bucket != "" && opts.ObjectPrefix != ""
}

type Manager interface {
	CreateDaemon(opts *DaemonCreateOpt) error
	GetDaemon(id string) *Daemon
	CleanupDaemon(daemonID string) error
	ListDaemons() []DaemonInfo
	ChunkDBStats() (*ChunkDBStats, error)
	LocalityStats() (*LocalityStats, error)
}

type manager struct {
	mu  sync.RWMutex
	ctx context.Context

	binPath                   string
	nodeID                    string
	root                      string
	ossCfgTemplate            BackendConfig // OSS backend config template
	nydusCfgTemplate          BackendConfig // Nydus backend config template
	daemons                   map[string]*Daemon
	nydusClient               NydusClient         // Client for fetching Nydus images
	ossAuths                  OSSAuthsConfig      // OSS authentication credentials
	registryAuths             registryauth.Config // Registry authentication credentials
	cgroupCtrl                *cgroup.Controller  // Memory cgroup for daemon processes (nil = disabled)
	nydusReadaheadWorkers     int
	nydusReadaheadWindowBytes int
	nydusDecodedCacheBytes    int
}

// NydusClient interface for fetching Nydus images and extracting bootstrap.
// FetchAndExtractBootstrap returns the bootstrap path and environment variables
// parsed from the image config.
type NydusClient interface {
	FetchAndExtractBootstrap(ctx context.Context, imageURL string, outputDir string) (string, []string, error)
}

type authenticatedNydusClient interface {
	FetchAndExtractBootstrapWithDockerConfigJSON(ctx context.Context, imageURL, outputDir, dockerConfigJSON string) (string, []string, error)
}

type insecureRegistryResolver interface {
	UseHTTPFor(imageURL string) bool
}

// ChunkDBStats represents the output of 'imagefsd stats-chunk' command
type ChunkDBStats struct {
	AccessTime struct {
		NewestEpochSecs int64 `json:"newest_epoch_secs"`
		OldestEpochSecs int64 `json:"oldest_epoch_secs"`
	} `json:"access_time"`
	Chunks struct {
		TotalCount int64 `json:"total_count"`
	} `json:"chunks"`
	Readers struct {
		Current      int64 `json:"current"`
		Max          int64 `json:"max"`
		StaleCleared int64 `json:"stale_cleared"`
	} `json:"readers"`
	Storage struct {
		FreeSizeBytes  int64  `json:"free_size_bytes"`
		TotalSizeBytes int64  `json:"total_size_bytes"`
		UsagePercent   string `json:"usage_percent"`
		UsedSizeBytes  int64  `json:"used_size_bytes"`
	} `json:"storage"`
}

type LocalityStats struct {
	ChunkDBTotalChunks        int64 `json:"chunkdb_total_chunks"`
	ChunkDBUsedBytes          int64 `json:"chunkdb_used_bytes"`
	ChunkDBRecentAccessAgeSec int64 `json:"chunkdb_recent_access_age_secs"`
	PeerHealthyCount          int64 `json:"peer_healthy_count"`
	PeerUnhealthyCount        int64 `json:"peer_unhealthy_count"`
	PeerHintedCount           int64 `json:"peer_hinted_count"`
}

// ManagerConfig holds configuration for creating a new Manager
type ManagerConfig struct {
	Context                   context.Context // Context for tracing and cancellation (optional, defaults to Background)
	NodeID                    string          // Stable control-plane node identity attached to imagefsd metrics.
	Root                      string          // Root working directory
	OSSCfgPath                string          // Path to OSS config template file
	NydusCfgPath              string          // Path to Nydus config template file
	BinPath                   string          // Path to imagefsd binary
	NydusClient               NydusClient     // Client for fetching Nydus images
	OSSAuthsPath              string          // Path to OSS auths file (oss_auths.json)
	RegistryAuthsPath         string          // Path to registry auths file (registry_auths.json)
	CgroupMemoryLimit         int64           // Memory limit in bytes for imagefsd cgroup (0 = no limit)
	NydusReadaheadWorkers     int             // Background workers for demand-triggered Nydus cache readahead.
	NydusReadaheadWindowBytes int             // Maximum bytes scheduled after a foreground read.
	NydusDecodedCacheBytes    int             // Per-mount decoded Nydus chunk cache limit.
}

func NewManager(config *ManagerConfig) (Manager, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if strings.TrimSpace(config.NodeID) == "" {
		return nil, fmt.Errorf("node ID is required")
	}

	// Default to background context if not provided
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	mgr := &manager{
		ctx:                       ctx,
		nodeID:                    strings.TrimSpace(config.NodeID),
		binPath:                   config.BinPath,
		root:                      config.Root,
		ossCfgTemplate:            BackendConfig{},
		nydusCfgTemplate:          BackendConfig{},
		daemons:                   map[string]*Daemon{},
		nydusClient:               config.NydusClient,
		cgroupCtrl:                cgroup.NewController(config.CgroupMemoryLimit),
		nydusReadaheadWorkers:     config.NydusReadaheadWorkers,
		nydusReadaheadWindowBytes: config.NydusReadaheadWindowBytes,
		nydusDecodedCacheBytes:    config.NydusDecodedCacheBytes,
	}
	if err := mgr.prepare(config.OSSCfgPath, config.NydusCfgPath, config.OSSAuthsPath, config.RegistryAuthsPath); err != nil {
		return nil, fmt.Errorf("failed to prepare imagefsd manager: %w", err)
	}
	if err := mgr.loadExistedDaemons(); err != nil {
		return nil, fmt.Errorf("failed to load existed daemons: %w", err)
	}
	mgr.addExistingDaemonsToCgroup()
	go mgr.gcWorker()
	go mgr.chunkDBCleanupWorker()
	return mgr, nil
}
