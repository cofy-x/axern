package langruntime

import (
	"fmt"
	"sync"
	"time"

	runtime_api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type RootFS struct {
	cfg          RootfsConfig
	path         string
	env          []string
	imageConfig  *ImageConfig
	mounter      ImageMounter
	cleanupFunc  func()
	mu           sync.Mutex // mu protects fields below
	activeRefs   int64
	retainedRefs int64
	deleted      bool
}

type ImageConfig struct {
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
	User       string   `json:"user,omitempty"`
}

type RootfsConfig struct {
	SrcType runtime_api.RootfsSrcType

	// OSS
	Endpoint        string
	Bucket          string
	Object          string
	AccessKeyID     string
	AccessKeySecret string

	// docker image
	ImageUrl         string
	ImageCacheKey    string
	DockerConfigJSON string
	LeaseID          string

	// local
	Path string
}

// String keeps diagnostics useful without exposing registry or object-store
// credentials carried by the runtime configuration.
func (cfg RootfsConfig) String() string {
	return fmt.Sprintf("{source_type:%s image_url:%q image_cache_key:%q endpoint:%q bucket:%q object:%q path:%q lease_id:%q has_registry_auth:%t has_object_credentials:%t}",
		cfg.SrcType.String(), cfg.ImageUrl, cfg.ImageCacheKey, cfg.Endpoint, cfg.Bucket, cfg.Object, cfg.Path, cfg.LeaseID,
		cfg.DockerConfigJSON != "", cfg.AccessKeyID != "" || cfg.AccessKeySecret != "")
}

func (rf *RootFS) Path() string {
	return rf.path
}

func (rf *RootFS) Env() []string {
	return rf.env
}

func (rf *RootFS) DefaultCommand() []string {
	if rf == nil || rf.imageConfig == nil {
		return nil
	}
	out := append([]string(nil), rf.imageConfig.Entrypoint...)
	out = append(out, rf.imageConfig.Cmd...)
	return out
}

func (rf *RootFS) WorkingDir() string {
	if rf == nil || rf.imageConfig == nil {
		return ""
	}
	return rf.imageConfig.WorkingDir
}

func (rf *RootFS) Config() RootfsConfig {
	return rf.cfg
}

func NewRootFS(cfg RootfsConfig, mounter ImageMounter, cleanup func()) (*RootFS, error) {
	rootfs, _, err := NewRootFSWithReport(cfg, mounter, cleanup)
	return rootfs, err
}

func NewRootFSWithReport(cfg RootfsConfig, mounter ImageMounter, cleanup func()) (*RootFS, RootfsPrepareReport, error) {
	report := RootfsPrepareReport{Steps: make([]RootfsStepSample, 0, 1)}
	rootFS := &RootFS{
		cfg:         cfg,
		mounter:     mounter,
		cleanupFunc: cleanup,
	}

	mountStart := time.Now()
	if err := rootFS.MountImage(); err != nil {
		report.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsMount, mountStart)
		return nil, report, err
	}
	report.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsMount, mountStart)

	return rootFS, report, nil
}
