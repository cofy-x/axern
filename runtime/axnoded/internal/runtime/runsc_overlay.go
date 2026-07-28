package runtime

import (
	"fmt"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func (r *RunscServiceHandler) overlay2Value(bundleRootReadonly bool, hostRootPathReadonly bool) string {
	if bundleRootReadonly {
		return ""
	}
	if r.filestoreDir != "" {
		return "root:dir=" + r.filestoreDir
	}
	if !hostRootPathReadonly {
		return ""
	}

	value := "root:memory"
	if r.overlayTmpfsSize != "" {
		value += ",size=" + r.overlayTmpfsSize
	}
	return value
}

func (r *RunscServiceHandler) overlayArgsForBundle(bundlePath string) ([]string, error) {
	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		return nil, fmt.Errorf("load bundle spec for runsc overlay: %w", err)
	}
	if ociSpec.Root == nil || ociSpec.Root.Readonly {
		return nil, nil
	}
	if r.filestoreDir != "" {
		return []string{"--overlay2", r.overlay2Value(false, false)}, nil
	}
	rootPath := ociSpec.Root.Path
	if rootPath == "" {
		return nil, nil
	}
	if !filepath.IsAbs(rootPath) {
		rootPath = filepath.Join(bundlePath, rootPath)
	}

	hostRootPathReadonly, err := hostlinux.IsPathReadOnly(rootPath)
	if err != nil {
		return nil, fmt.Errorf("detect rootfs mount mode for %s: %w", rootPath, err)
	}

	value := r.overlay2Value(false, hostRootPathReadonly)
	if value == "" {
		return nil, nil
	}
	return []string{"--overlay2", value}, nil
}
