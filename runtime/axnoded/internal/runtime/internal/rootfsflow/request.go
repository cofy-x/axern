package rootfsflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

type RuntimePolicy struct {
	RuntimeName             string
	NeedsHostWritableRootfs bool
	WritableLayerLimitBytes int64
	ProjectID               uint32
	RootfsLeaseID           string
}

// PrepareBundle creates a sandbox-private rootfs projection after the final OCI
// mounts are known. It never creates mount targets in the input rootfs.
func PrepareBundle(ctx context.Context, provider rootfsview.Provider, options contract.HandlerOptions, bundlePath string, policy RuntimePolicy) (bool, error) {
	specPath := filepath.Join(bundlePath, config.ContainerSpecFile)
	ociSpec, err := runtimeoci.LoadSpec(specPath)
	if err != nil {
		return false, fmt.Errorf("load bundle spec for rootfs projection: %w", err)
	}
	if ociSpec.Root == nil || strings.TrimSpace(ociSpec.Root.Path) == "" {
		return false, fmt.Errorf("bundle rootfs is required")
	}
	rootfsPath := strings.TrimSpace(ociSpec.Root.Path)
	if !filepath.IsAbs(rootfsPath) {
		rootfsPath = filepath.Join(bundlePath, rootfsPath)
	}

	targets := make([]rootfsview.MountTarget, 0, len(ociSpec.Mounts))
	for _, mount := range ociSpec.Mounts {
		if mount.Type != "bind" {
			continue
		}
		info, err := os.Stat(mount.Source)
		if err != nil {
			return false, fmt.Errorf("stat bind mount source %q: %w", mount.Source, err)
		}
		kind := rootfsview.TargetRegularFile
		switch {
		case info.IsDir():
			kind = rootfsview.TargetDirectory
		case info.Mode().IsRegular():
		default:
			return false, fmt.Errorf("bind mount source %q is neither a regular file nor a directory", mount.Source)
		}
		targets = append(targets, rootfsview.MountTarget{Destination: mount.Destination, Kind: kind})
	}

	prepareStart := time.Now()
	backing, err := rootfsview.InspectBacking(rootfsPath)
	if err != nil {
		return false, fmt.Errorf("inspect rootfs backing: %w", err)
	}
	view, err := provider.Prepare(ctx, options.ContainerID, rootfsview.Request{
		RootDir: rootfsPath, Readonly: ociSpec.Root.Readonly, RuntimeName: policy.RuntimeName,
		NeedsHostWritableRootfs: policy.NeedsHostWritableRootfs, Backing: backing, Targets: targets,
		WritableLayerLimitBytes: policy.WritableLayerLimitBytes, ProjectID: policy.ProjectID,
		RootfsLeaseID: policy.RootfsLeaseID,
	})
	options.RecordStartupStep(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsViewPrepare, time.Since(prepareStart))
	if err != nil {
		return false, fmt.Errorf("prepare sandbox-private rootfs failed: %w", err)
	}
	if !view.Prepared {
		return false, nil
	}
	if strings.TrimSpace(view.RootDir) == "" {
		_ = provider.Remove(context.Background(), options.ContainerID)
		return false, fmt.Errorf("prepared rootfs view root dir is required")
	}

	applyStart := time.Now()
	ociSpec.Root.Path = view.RootDir
	if err := runtimeoci.WriteSpecAtomic(specPath, ociSpec); err != nil {
		_ = provider.Remove(context.Background(), options.ContainerID)
		return false, fmt.Errorf("write projected rootfs bundle spec: %w", err)
	}
	options.RecordStartupStep(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsViewApply, time.Since(applyStart))
	return true, nil
}
