package runtime

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
)

func ensureRuntimeFilestore(cfg config.Config) (string, error) {
	if cfg.RuntimeConfig.FilestoreDir == "" {
		return "", nil
	}
	if cfg.RuntimeConfig.FilestoreDirSize == "" {
		return "", fmt.Errorf("filestore_dir_size must be set when filestore_dir is configured")
	}
	if err := hostlinux.EnsureXFSMount(cfg.RuntimeConfig.FilestoreDir, cfg.RuntimeConfig.FilestoreDirSize); err != nil {
		return "", err
	}
	return cfg.RuntimeConfig.FilestoreDir, nil
}
