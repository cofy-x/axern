package imagefsd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/cgroup"
)

const (
	daemonExpiredPeriod     = 30 * time.Minute
	mountReadinessLogPeriod = time.Second
)

// Timeout configurations for daemon operations
var (
	daemonMountTimeout   = 60 * time.Second
	daemonUnmountTimeout = 60 * time.Second
	statfsFunc           = unix.Statfs
)

// DaemonState represents the lifecycle state of a daemon
type DaemonState int32

const (
	DaemonStateStopped DaemonState = iota
	DaemonStateMounting
	DaemonStateRunning
	DaemonStateUnmounting
)

// Stopper provides a thread-safe way to signal daemon stop
type Stopper struct {
	done chan struct{}
	once sync.Once
}

// NewStopper creates a new Stopper
func NewStopper() *Stopper {
	return &Stopper{done: make(chan struct{})}
}

// Done returns a receive-only channel that will be closed when Close is called
func (s *Stopper) Done() <-chan struct{} {
	return s.done
}

// Close safely closes the done channel, can be called multiple times
func (s *Stopper) Close() {
	s.once.Do(func() {
		close(s.done)
	})
}

type DaemonMeta struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	CfgPath              string   `json:"cfg_path"`
	MountPoint           string   `json:"mount_point"`
	DaemonDir            string   `json:"daemon_dir"`
	DaemonLogPath        string   `json:"daemon_log_path"`
	PidFilePath          string   `json:"pid_file_path"`
	CachePath            string   `json:"cache_path"`
	ImageMetaDir         string   `json:"image_meta_dir"`
	ChunkDBDir           string   `json:"chunk_db_dir"`
	SourceType           string   `json:"source_type,omitempty"`    // "oss" or "nydus"
	BootstrapPath        string   `json:"bootstrap_path,omitempty"` // For Nydus: path to bootstrap file
	CacheDir             string   `json:"cache_dir,omitempty"`      // For Nydus: --cache-dir parameter
	ReadaheadWorkers     int      `json:"readahead_workers,omitempty"`
	ReadaheadWindowBytes int      `json:"readahead_window_bytes,omitempty"`
	DecodedCacheBytes    int      `json:"decoded_cache_bytes,omitempty"`
	ImageURL             string   `json:"image_url,omitempty"`    // For Nydus: image URL for bootstrap download
	Env                  []string `json:"env,omitempty"`          // Environment variables from image config
	EnvResolved          bool     `json:"env_resolved,omitempty"` // True after env has been extracted (distinguishes nil env from unresolved)
}

// DaemonInfo contains basic information about a daemon
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

type Daemon struct {
	mu               sync.Mutex
	ctx              context.Context
	meta             DaemonMeta
	binPath          string
	nodeID           string
	config           *BackendConfig // Backend configuration (OSS, Nydus/Registry, etc.)
	savedPath        string
	stopChan         chan struct{}
	kickStop         *Stopper
	state            atomic.Int32 // DaemonState
	expiredAt        int64
	nydusClient      NydusClient        // For Nydus: client to fetch bootstrap
	dockerConfigJSON string             // Request-scoped bootstrap auth; never persisted.
	cgroupCtrl       *cgroup.Controller // Memory cgroup controller (nil = disabled)

	// watcherActive indicates if watcher goroutine is active
	watcherActive atomic.Bool

	// userStopped indicates the daemon was explicitly unmounted by user/API call.
	// When set, automatic remount is suppressed.
	userStopped atomic.Bool

	// mountFailed indicates a mount attempt timed out without success.
	// GC uses this to detect and clean up orphaned daemon processes.
	mountFailed atomic.Bool

	// isAliveFunc allows mocking IsAlive() for testing
	isAliveFunc func() bool
}

func (d *Daemon) daemonLogFields() logrus.Fields {
	fields := logrus.Fields{
		"daemon_id":   d.meta.ID,
		"daemon_name": d.meta.Name,
		"mount_point": d.meta.MountPoint,
		"source_type": normalizeSourceType(d.meta.SourceType),
	}
	if d.meta.ImageURL != "" {
		fields["image_url"] = d.meta.ImageURL
	}
	return fields
}

func (d *Daemon) MountPoint() string {
	return d.meta.MountPoint
}

func (d *Daemon) Env() []string {
	return d.meta.Env
}

func (d *Daemon) Name() string {
	return d.meta.Name
}

func (d *Daemon) getState() DaemonState {
	return DaemonState(d.state.Load())
}

func (d *Daemon) setState(state DaemonState) {
	d.state.Store(int32(state))
}

// compareAndSwapState atomically compares and swaps the state
func (d *Daemon) compareAndSwapState(old, new DaemonState) bool {
	return d.state.CompareAndSwap(int32(old), int32(new))
}

func (d *Daemon) getPid() int {
	info, err := os.ReadFile(d.meta.PidFilePath)
	if err != nil {
		return -1
	}
	pid, err := strconv.Atoi(strings.TrimRight(string(info), "\n"))
	if err != nil {
		logrus.WithFields(d.daemonLogFields()).Warn("can't parse daemon pid file")
		return -1
	}
	return pid
}

func (d *Daemon) IsAlive() bool {
	// Allow mocking for tests
	if d.isAliveFunc != nil {
		return d.isAliveFunc()
	}

	pid := d.getPid()
	if pid <= 0 {
		return false
	}
	binPath, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return false
	}
	return binPath == d.binPath
}

func (d *Daemon) updateExpired() {
	atomic.StoreInt64(&d.expiredAt, time.Now().Add(daemonExpiredPeriod).UnixNano())
}
