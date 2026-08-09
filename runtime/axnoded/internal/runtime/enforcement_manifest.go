package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/durablefile"
	"google.golang.org/protobuf/proto"
)

const runtimeEnforcementManifestFile = "runtime-enforcement.pb"

func writeRuntimeEnforcementManifest(bundlePath, runtimeName, filestoreDir string, request *apipb.CreateContainerRequest, options contract.HandlerOptions, overlayArg string, projectID uint32) error {
	if request == nil || bundlePath == "" || runtimeName == "" {
		return fmt.Errorf("runtime enforcement manifest requires bundle, runtime, and request")
	}
	manifest := &apipb.AllocationEnforcementManifest{
		RuntimeName: runtimeName, MemoryLimitBytes: options.MemoryLimitBytes,
		EphemeralStorageLimitBytes: request.GetEphemeralStorageLimitBytes(),
		CgroupPath:                 options.CgroupPath, RuntimeCgroupPath: options.RuntimeCgroupPath,
		RunscOverlayArg: overlayArg, RuncProjectID: projectID,
		BundlePath: bundlePath, CreatedAtUnixNano: time.Now().UTC().UnixNano(),
	}
	if request.GetEphemeralStorageLimitBytes() > 0 {
		if filestoreDir == "" {
			return fmt.Errorf("ephemeral storage enforcement manifest requires filestore directory")
		}
		facts, err := hostlinux.ReadFilestoreCapabilities(filestoreDir)
		if err != nil {
			return fmt.Errorf("read filestore identity for enforcement manifest: %w", err)
		}
		manifest.FilestoreMountIdentity = facts.MountIdentity
		if runtimeName == "runsc" {
			manifest.RunscBackingDirectory = filepath.Join(filestoreDir, "runsc")
			manifest.RunscBackingDirectoryIdentity, err = hostlinux.DirectoryIdentity(manifest.GetRunscBackingDirectory())
			if err != nil {
				return fmt.Errorf("read runsc backing directory identity for enforcement manifest: %w", err)
			}
		}
	}
	if err := contract.ValidateEnforcementManifest(manifest, bundlePath); err != nil {
		return err
	}
	content, err := proto.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal runtime enforcement manifest: %w", err)
	}
	if err := durablefile.Write(filepath.Join(bundlePath, runtimeEnforcementManifestFile), content, 0o600); err != nil {
		return fmt.Errorf("write runtime enforcement manifest: %w", err)
	}
	return nil
}

func readRuntimeEnforcementManifest(bundlePath string) (*apipb.AllocationEnforcementManifest, error) {
	content, err := os.ReadFile(filepath.Join(bundlePath, runtimeEnforcementManifestFile))
	if err != nil {
		return nil, err
	}
	var manifest apipb.AllocationEnforcementManifest
	if err := proto.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("decode runtime enforcement manifest: %w", err)
	}
	if err := contract.ValidateEnforcementManifest(&manifest, bundlePath); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (r *RuncServiceHandler) AllocationEnforcementManifest(_ context.Context, containerID string) (*apipb.AllocationEnforcementManifest, error) {
	return readRuntimeEnforcementManifest(filepath.Join(r.common.ContainerRoot(), containerID))
}

func (r *RunscServiceHandler) AllocationEnforcementManifest(_ context.Context, containerID string) (*apipb.AllocationEnforcementManifest, error) {
	return readRuntimeEnforcementManifest(filepath.Join(r.common.ContainerRoot(), containerID))
}
