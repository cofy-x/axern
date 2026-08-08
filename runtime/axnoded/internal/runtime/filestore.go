package runtime

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
)

func ensureRuntimeFilestore(cfg config.Config) (string, error) {
	if cfg.RuntimeConfig.FilestoreDir == "" {
		return "", nil
	}
	mode := cfg.RuntimeConfig.FilestoreMode
	if mode == "" {
		mode = config.FilestoreModeExisting
	}
	if err := hostlinux.PrepareFilestore(
		cfg.RuntimeConfig.FilestoreDir,
		mode,
		cfg.RuntimeConfig.FilestoreLoopbackImage,
		cfg.RuntimeConfig.FilestoreLoopbackSizeBytes,
		cfg.RuntimeConfig.FilestoreSystemReserveBytes,
	); err != nil {
		metrics.RecordFilestoreProbe("startup", "failure")
		return "", fmt.Errorf("prepare runtime filestore: %w", err)
	}
	metrics.RecordFilestoreProbe("startup", "success")
	if capabilities, err := hostlinux.ReadFilestoreCapabilities(cfg.RuntimeConfig.FilestoreDir); err == nil {
		metrics.RecordFilestoreProbe("overlay", probeResult(capabilities.OverlayReady))
		metrics.RecordFilestoreProbe("xfs_project_quota", probeResult(capabilities.ProjectQuotaReady))
		if capabilities.EROFSProbeError != "" || capabilities.EROFSReady {
			metrics.RecordFilestoreProbe("erofs_lower", probeResult(capabilities.EROFSReady))
		}
	}
	return cfg.RuntimeConfig.FilestoreDir, nil
}

func probeResult(ok bool) string {
	if ok {
		return "success"
	}
	return "failure"
}
