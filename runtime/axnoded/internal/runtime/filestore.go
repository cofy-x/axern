package runtime

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
)

type runtimeFilestoreConfig struct {
	dir           string
	mode          string
	image         string
	loopbackBytes int64
	systemReserve int64
}

type runtimeFilestoreState struct {
	config runtimeFilestoreConfig
	refs   int
}

var runtimeFilestores = struct {
	sync.Mutex
	byDir map[string]*runtimeFilestoreState
}{byDir: make(map[string]*runtimeFilestoreState)}

var (
	prepareRuntimeFilestore = hostlinux.PrepareFilestore
	cleanupRuntimeFilestore = hostlinux.CleanupFilestore
)

func acquireRuntimeFilestore(cfg config.Config) (string, func(bool), error) {
	if cfg.RuntimeConfig.FilestoreDir == "" {
		return "", func(bool) {}, nil
	}
	mode := cfg.RuntimeConfig.FilestoreMode
	if mode == "" {
		mode = config.FilestoreModeExisting
	}
	filestoreConfig := runtimeFilestoreConfig{
		dir: filepath.Clean(cfg.RuntimeConfig.FilestoreDir), mode: mode,
		image:         cfg.RuntimeConfig.FilestoreLoopbackImage,
		loopbackBytes: cfg.RuntimeConfig.FilestoreLoopbackSizeBytes,
		systemReserve: cfg.RuntimeConfig.FilestoreSystemReserveBytes,
	}
	runtimeFilestores.Lock()
	defer runtimeFilestores.Unlock()
	if current := runtimeFilestores.byDir[filestoreConfig.dir]; current != nil {
		if current.config != filestoreConfig {
			return "", nil, fmt.Errorf("filestore %s is already acquired with conflicting configuration", filestoreConfig.dir)
		}
		current.refs++
		return filestoreConfig.dir, runtimeFilestoreReleaseFunc(filestoreConfig.dir), nil
	}
	if err := prepareRuntimeFilestore(
		filestoreConfig.dir,
		mode,
		filestoreConfig.image,
		filestoreConfig.loopbackBytes,
		filestoreConfig.systemReserve,
	); err != nil {
		metrics.RecordFilestoreProbe("startup", "failure")
		return "", nil, fmt.Errorf("prepare runtime filestore: %w", err)
	}
	runtimeFilestores.byDir[filestoreConfig.dir] = &runtimeFilestoreState{config: filestoreConfig, refs: 1}
	metrics.RecordFilestoreProbe("startup", "success")
	if capabilities, err := hostlinux.ReadFilestoreCapabilities(filestoreConfig.dir); err == nil {
		metrics.RecordFilestoreProbe("overlay", probeResult(capabilities.OverlayReady))
		metrics.RecordFilestoreProbe("xfs_project_quota", probeResult(capabilities.ProjectQuotaReady))
		if capabilities.EROFSProbeError != "" || capabilities.EROFSReady {
			metrics.RecordFilestoreProbe("erofs_lower", probeResult(capabilities.EROFSReady))
		}
	}
	return filestoreConfig.dir, runtimeFilestoreReleaseFunc(filestoreConfig.dir), nil
}

func runtimeFilestoreReleaseFunc(dir string) func(bool) {
	var once sync.Once
	return func(cleanupOnLastRelease bool) {
		once.Do(func() {
			_ = releaseRuntimeFilestore(dir, cleanupOnLastRelease)
		})
	}
}

func releaseRuntimeFilestore(dir string, cleanupOnLastRelease bool) error {
	runtimeFilestores.Lock()
	defer runtimeFilestores.Unlock()
	state := runtimeFilestores.byDir[filepath.Clean(dir)]
	if state == nil {
		return nil
	}
	state.refs--
	if state.refs > 0 {
		return nil
	}
	if cleanupOnLastRelease {
		if err := cleanupRuntimeFilestore(state.config.dir, state.config.mode, state.config.image); err != nil {
			// Preserve the zero-ref state: a later runtime construction may adopt
			// the still-mounted filestore, while service shutdown remains the final
			// lifecycle owner that retries cleanup.
			state.refs = 0
			return fmt.Errorf("cleanup runtime filestore after failed runtime construction: %w", err)
		}
	}
	delete(runtimeFilestores.byDir, state.config.dir)
	return nil
}

func probeResult(ok bool) string {
	if ok {
		return "success"
	}
	return "failure"
}
