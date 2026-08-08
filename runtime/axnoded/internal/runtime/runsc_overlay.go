package runtime

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func (r *RunscServiceHandler) overlay2Value(bundleRootReadonly bool, limitBytes int64) (string, error) {
	if bundleRootReadonly {
		return "", nil
	}
	if r.filestoreDir == "" {
		return "", fmt.Errorf("writable runsc rootfs requires runtime filestore_dir")
	}
	if limitBytes <= 0 {
		return "", fmt.Errorf("writable runsc rootfs requires writable_layer_limit_bytes > 0")
	}
	return "root:dir=" + filepath.Join(r.filestoreDir, "runsc") +
		",size=" + strconv.FormatInt(limitBytes, 10), nil
}

func (r *RunscServiceHandler) overlayArgsForBundle(bundlePath string, limitBytes int64) ([]string, error) {
	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		return nil, fmt.Errorf("load bundle spec for runsc overlay: %w", err)
	}
	if ociSpec.Root == nil {
		return nil, fmt.Errorf("runsc bundle rootfs is required")
	}
	value, err := r.overlay2Value(ociSpec.Root.Readonly, limitBytes)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	}
	return []string{"--overlay2", value}, nil
}
