package contract

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

var directoryIdentityPattern = regexp.MustCompile("^devino:v1:[0-9a-f]+:[0-9a-f]+$")

// ValidateEnforcementManifest validates the immutable contract independently
// of the mutable runtime state it will later be compared against.
func ValidateEnforcementManifest(manifest *apipb.AllocationEnforcementManifest, expectedBundlePath string) error {
	if manifest == nil {
		return fmt.Errorf("allocation enforcement manifest is required")
	}
	if manifest.GetRuntimeName() != "runc" && manifest.GetRuntimeName() != "runsc" {
		return fmt.Errorf("allocation enforcement manifest has unsupported runtime %q", manifest.GetRuntimeName())
	}
	if manifest.GetCreatedAtUnixNano() <= 0 {
		return fmt.Errorf("allocation enforcement manifest created_at is required")
	}
	if manifest.GetMemoryLimitBytes() < 0 || manifest.GetEphemeralStorageLimitBytes() < 0 {
		return fmt.Errorf("allocation enforcement manifest limits cannot be negative")
	}
	if manifest.GetMemoryLimitBytes() > 0 {
		for name, value := range map[string]string{
			"cgroup_path": manifest.GetCgroupPath(), "runtime_cgroup_path": manifest.GetRuntimeCgroupPath(),
		} {
			cleaned := filepath.Clean(strings.TrimSpace(value))
			if !filepath.IsAbs(cleaned) || cleaned == string(filepath.Separator) || cleaned != value || len(value) > 1024 {
				return fmt.Errorf("memory enforcement manifest requires a clean bounded %s", name)
			}
		}
		if strings.TrimSpace(manifest.GetCgroupBootID()) == "" || len(manifest.GetCgroupBootID()) > 128 {
			return fmt.Errorf("memory enforcement manifest requires bounded cgroup boot identity")
		}
		if strings.TrimSpace(manifest.GetCgroupMountIdentity()) == "" || len(manifest.GetCgroupMountIdentity()) > 1024 {
			return fmt.Errorf("memory enforcement manifest requires bounded cgroup mount identity")
		}
		if manifest.GetCgroupParentInode() == 0 || manifest.GetCgroupLeafInode() == 0 || manifest.GetCgroupParentInode() == manifest.GetCgroupLeafInode() {
			return fmt.Errorf("memory enforcement manifest requires distinct parent and leaf identities")
		}
		if manifest.GetMemorySwapMaxBytes() != 0 || !manifest.GetMemoryOomGroup() {
			return fmt.Errorf("memory enforcement manifest requires swap disabled and group OOM")
		}
	} else if manifest.GetCgroupBootID() != "" || manifest.GetCgroupMountIdentity() != "" || manifest.GetCgroupParentInode() != 0 || manifest.GetCgroupLeafInode() != 0 || manifest.GetMemorySwapMaxBytes() != 0 || manifest.GetMemoryOomGroup() {
		return fmt.Errorf("unlimited allocation enforcement manifest contains cgroup memory state")
	}
	bundlePath := filepath.Clean(strings.TrimSpace(manifest.GetBundlePath()))
	if !filepath.IsAbs(bundlePath) || bundlePath == string(filepath.Separator) || bundlePath != manifest.GetBundlePath() {
		return fmt.Errorf("allocation enforcement manifest bundle_path must be a clean absolute path")
	}
	if expectedBundlePath != "" && bundlePath != filepath.Clean(expectedBundlePath) {
		return fmt.Errorf("allocation enforcement manifest bundle_path does not match runtime bundle")
	}

	limit := manifest.GetEphemeralStorageLimitBytes()
	if limit == 0 {
		if manifest.GetFilestoreMountIdentity() != "" || manifest.GetRuncProjectID() != 0 || manifest.GetRunscOverlayArg() != "" || manifest.GetRunscBackingDirectory() != "" || manifest.GetRunscBackingDirectoryIdentity() != "" {
			return fmt.Errorf("readonly allocation enforcement manifest contains ephemeral-storage state")
		}
		return nil
	}
	if strings.TrimSpace(manifest.GetFilestoreMountIdentity()) == "" || len(manifest.GetFilestoreMountIdentity()) > 1024 {
		return fmt.Errorf("ephemeral-storage enforcement manifest requires a bounded filestore mount identity")
	}

	if manifest.GetRuntimeName() == "runc" {
		if manifest.GetRuncProjectID() == 0 {
			return fmt.Errorf("runc ephemeral-storage enforcement manifest requires a project ID")
		}
		if manifest.GetRunscOverlayArg() != "" || manifest.GetRunscBackingDirectory() != "" || manifest.GetRunscBackingDirectoryIdentity() != "" {
			return fmt.Errorf("runc enforcement manifest contains runsc state")
		}
		return nil
	}

	backingDirectory := filepath.Clean(strings.TrimSpace(manifest.GetRunscBackingDirectory()))
	if !filepath.IsAbs(backingDirectory) || backingDirectory == string(filepath.Separator) || backingDirectory != manifest.GetRunscBackingDirectory() {
		return fmt.Errorf("runsc enforcement manifest requires a clean absolute backing directory")
	}
	if !directoryIdentityPattern.MatchString(manifest.GetRunscBackingDirectoryIdentity()) {
		return fmt.Errorf("runsc enforcement manifest has invalid backing directory identity")
	}
	expectedOverlay := "root:dir=" + backingDirectory + ",size=" + strconv.FormatInt(limit, 10)
	if manifest.GetRunscOverlayArg() != expectedOverlay {
		return fmt.Errorf("runsc enforcement manifest overlay argument does not match its backing and limit")
	}
	if manifest.GetRuncProjectID() != 0 {
		return fmt.Errorf("runsc enforcement manifest contains a runc project ID")
	}
	return nil
}
