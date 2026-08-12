package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/rootfssupport"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

const (
	containerNamePrefix = "axern-oci-"

	// PruneInterval is the interval between background disk-pressure GC checks.
	PruneInterval = 2 * time.Minute
	// defaultGlobalLayerWorkers limits global concurrent layer extraction.
	defaultGlobalLayerWorkers = 4
	// defaultLayerZeroRefTTL is the default TTL for unreferenced layers.
	defaultLayerZeroRefTTL = 30 * time.Minute

	diskUsageGCStart = 0.85
	diskUsageGCStop  = 0.75
)

// Config holds the OCI configuration parsed from config file.
type Config struct {
	Proxy *ProxyConfig `json:"proxy,omitempty"`
}

// ProxyConfig mirrors imagefsd.ProxyConfig for config parsing.
type ProxyConfig struct {
	Url string `json:"url"`
}

// Manager manages local OCI layer extraction and readonly overlay mounts.
type Manager struct {
	root       string
	layersDir  string
	chainsDir  string
	mountsDir  string
	importsDir string
	supportDir string
	proxy      string

	registry *imageregistry.Client
	store    *metadataStore

	mutex sync.Mutex
	// image_url -> container info
	containers map[string]*ContainerInfo
	imageLocks map[string]*imageLockEntry
	layerLocks map[string]*imageLockEntry
	chainLocks map[string]*imageLockEntry

	stopOnce sync.Once
	stopCh   chan struct{}

	now       func() time.Time
	mountFn   func(target string, lowerDirs []string) error
	unmountFn func(target string) error
	diskUsage func(path string) (float64, error)
	readMnts  func() (managedMountSnapshot, error)

	layerWorkers  int
	layerJobs     chan layerExtractJob
	layerPoolWG   sync.WaitGroup
	layerPoolOnce sync.Once
	layerPoolMu   sync.Mutex
	layerTTL      time.Duration
}

type imageLockEntry struct {
	mu   sync.Mutex
	refs int
}

// ContainerInfo stores mount-related information.
type ContainerInfo struct {
	MountID      string
	ImageURL     string
	MountPath    string
	LayerDigests []string
	ChainIDs     []string
	LowerDirs    []string
	Env          []string
	ImageConfig  *ImageConfig
}

type ImageConfig struct {
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
	User       string   `json:"user,omitempty"`
}

type MountResult struct {
	MountPath   string
	Env         []string
	ImageConfig *ImageConfig
	MountID     string
	LowerDirs   []string
}

// NewManager creates a new OCI manager.
// sharedRegistryClient is optional. If nil, an anonymous registry client is used.
func NewManager(rootWorkDir string, cfgTempPath string, sharedRegistryClient ...*imageregistry.Client) (*Manager, error) {
	root := filepath.Join(rootWorkDir, "oci")
	layersDir := filepath.Join(root, "layers")
	chainsDir := filepath.Join(root, "lowerdirs")
	mountsDir := filepath.Join(root, "mounts")
	importsDir := filepath.Join(root, "imports")
	supportDir := filepath.Join(root, "support", "fs")
	dbPath := filepath.Join(root, "metadata.db")

	var proxy string
	if cfgTempPath != "" {
		cfg, err := loadConfig(cfgTempPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Proxy != nil {
			proxy = cfg.Proxy.Url
			logrus.Infof("OCI manager HTTP proxy: %s", proxy)
		}
	}

	if err := os.MkdirAll(layersDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create layers dir %s: %w", layersDir, err)
	}
	if err := os.MkdirAll(chainsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lowerdirs dir %s: %w", chainsDir, err)
	}
	if err := os.MkdirAll(mountsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mounts dir %s: %w", mountsDir, err)
	}
	if err := os.MkdirAll(importsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create imports dir %s: %w", importsDir, err)
	}
	if err := rootfssupport.Ensure(supportDir); err != nil {
		return nil, fmt.Errorf("failed to prepare runtime support dir %s: %w", supportDir, err)
	}

	store, err := openMetadataStore(dbPath)
	if err != nil {
		return nil, err
	}

	var registryClient *imageregistry.Client
	if len(sharedRegistryClient) > 0 && sharedRegistryClient[0] != nil {
		registryClient = sharedRegistryClient[0]
	} else {
		registryClient, err = imageregistry.NewClient("")
		if err != nil {
			store.close()
			return nil, fmt.Errorf("failed to create registry client: %w", err)
		}
	}

	mgr := &Manager{
		root:       root,
		layersDir:  layersDir,
		chainsDir:  chainsDir,
		mountsDir:  mountsDir,
		importsDir: importsDir,
		supportDir: supportDir,
		proxy:      proxy,
		registry:   registryClient,
		store:      store,
		containers: make(map[string]*ContainerInfo),
		imageLocks: make(map[string]*imageLockEntry),
		layerLocks: make(map[string]*imageLockEntry),
		chainLocks: make(map[string]*imageLockEntry),
		stopCh:     make(chan struct{}),
		now:        time.Now,
		mountFn:    defaultOverlayMount,
		unmountFn:  defaultOverlayUnmount,
		diskUsage:  defaultDiskUsage,
		readMnts: func() (managedMountSnapshot, error) {
			return readManagedMounts(mountsDir)
		},
		layerWorkers: defaultGlobalLayerWorkers,
		layerTTL:     defaultLayerZeroRefTTL,
	}

	if err := mgr.reconcileState(); err != nil {
		logrus.Warnf("failed to reconcile OCI metadata at startup: %v", err)
	}

	go mgr.pruneImagesLoop()
	return mgr, nil
}

// loadConfig loads the configuration from the given file path.
func loadConfig(cfgPath string) (*Config, error) {
	file, err := os.Open(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	if err = json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	return &cfg, nil
}

// MountImage pulls and extracts OCI layers, then mounts a readonly overlay rootfs.
func (m *Manager) MountImage(imageURL string) (*MountResult, error) {
	return m.MountImageWithContext(context.Background(), imageURL)
}

func (m *Manager) MountImageWithAuth(imageURL, dockerConfigJSON string) (*MountResult, error) {
	return m.MountImageWithContextAndAuth(context.Background(), imageURL, dockerConfigJSON)
}

func (m *Manager) UnmountImage(imageURL string) error {
	return m.UnmountImageWithContext(context.Background(), imageURL)
}

// ListMountedImageURLs returns all currently mounted OCI image URLs.
func (m *Manager) ListMountedImageURLs() ([]string, error) {
	details, err := m.ListMountedDetails()
	if err != nil {
		return nil, err
	}
	imageURLs := make([]string, 0, len(details))
	for _, rec := range details {
		imageURLs = append(imageURLs, rec.ImageURL)
	}
	return imageURLs, nil
}

// ListMountedDetails returns detailed metadata of all currently mounted OCI images.
func (m *Manager) ListMountedDetails() ([]OciMountRecord, error) {
	records, err := m.store.listMounts()
	if err != nil {
		return nil, fmt.Errorf("failed to list oci mounts: %w", err)
	}
	details := make([]OciMountRecord, 0, len(records))
	for _, rec := range records {
		if rec == nil || rec.ImageURL == "" {
			continue
		}
		details = append(details, OciMountRecord{
			CacheKey:      rec.CacheKey,
			ImageURL:      rec.ImageURL,
			MountID:       rec.MountID,
			MountPath:     rec.MountPath,
			LayerDigests:  append([]string(nil), rec.LayerDigests...),
			ChainIDs:      append([]string(nil), rec.ChainIDs...),
			LowerDirs:     append([]string(nil), rec.LowerDirs...),
			CreatedAtUnix: rec.CreatedAtUnix,
		})
	}
	sort.Slice(details, func(i, j int) bool {
		return details[i].ImageURL < details[j].ImageURL
	})
	return details, nil
}

// Close stops background workers and releases resources.
func (m *Manager) Close() error {
	m.layerPoolMu.Lock()
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.layerPoolWG.Wait()
	m.layerPoolMu.Unlock()

	m.mutex.Lock()
	mounts := make(map[string]string, len(m.containers))
	for cacheKey, info := range m.containers {
		imageURL := cacheKey
		if info != nil && info.ImageURL != "" {
			imageURL = info.ImageURL
		}
		mounts[cacheKey] = imageURL
	}
	m.mutex.Unlock()

	for cacheKey, imageURL := range mounts {
		if err := m.UnmountImageWithContextAndKey(context.Background(), imageURL, cacheKey); err != nil {
			logrus.Warnf("failed to unmount image %s (%s) during shutdown: %v", imageURL, cacheKey, err)
		}
	}

	return m.store.close()
}
