package startplan

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func MaterializeResolvedSecretFiles(request *runtime.StartRequest) ([]*runtime.Mount, func(), error) {
	if request == nil || len(request.GetSecretFiles()) == 0 {
		return nil, func() {}, nil
	}
	if strings.TrimSpace(request.GetAllocationID()) == "" {
		return nil, nil, fmt.Errorf("allocation ID is required for secret files")
	}
	secretRoot := resolvedSecretRoot(request.GetAllocationID())
	if err := os.RemoveAll(secretRoot); err != nil {
		return nil, nil, fmt.Errorf("cleanup previous secret root: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(secretRoot) }
	mounts := make([]*runtime.Mount, 0, len(request.GetSecretFiles()))
	seenTargets := make(map[string]struct{}, len(request.GetSecretFiles()))
	for _, item := range request.GetSecretFiles() {
		if item == nil {
			cleanup()
			return nil, nil, fmt.Errorf("resolved secret file is required")
		}
		rawTarget := strings.TrimSpace(item.Path)
		target := path.Clean(rawTarget)
		if rawTarget == "" || target == "/" || !strings.HasPrefix(target, "/") || hasParentPathElement(rawTarget) {
			cleanup()
			return nil, nil, fmt.Errorf("resolved secret file path %q must be an absolute container path below /", rawTarget)
		}
		if protectedSecretFileTarget(target) {
			cleanup()
			return nil, nil, fmt.Errorf("resolved secret file path %q overlaps a protected runtime path", target)
		}
		if item.GetMode() > 0o777 || item.GetMode()&0o222 != 0 {
			cleanup()
			return nil, nil, fmt.Errorf("resolved secret file %q mode must be read-only permissions within 0777", target)
		}
		for existing := range seenTargets {
			if target == existing || strings.HasPrefix(target, existing+"/") || strings.HasPrefix(existing, target+"/") {
				cleanup()
				return nil, nil, fmt.Errorf("resolved secret file path %q overlaps path %q", target, existing)
			}
		}
		seenTargets[target] = struct{}{}
		rel := strings.TrimPrefix(target, "/")
		hostPath := filepath.Join(secretRoot, rel)
		if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("create secret dir: %w", err)
		}
		mode := os.FileMode(item.Mode)
		if mode == 0 {
			mode = 0o400
		}
		if err := os.WriteFile(hostPath, item.GetContent(), mode); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("write secret file %s: %w", target, err)
		}
		mounts = append(mounts, &runtime.Mount{
			Type:    "bind",
			Source:  hostPath,
			Target:  target,
			Options: []string{"ro"},
		})
	}
	return mounts, cleanup, nil
}

func protectedSecretFileTarget(target string) bool {
	for _, protected := range []string{"/bin", "/boot", "/dev", "/lib", "/lib64", "/proc", "/sbin", "/sys", "/usr", "/run/axern", "/var/run/axern"} {
		if target == protected || strings.HasPrefix(target, protected+"/") || strings.HasPrefix(protected, target+"/") {
			return true
		}
	}
	for _, protected := range []string{"/etc/group", "/etc/gshadow", "/etc/hostname", "/etc/hosts", "/etc/ld.so.preload", "/etc/passwd", "/etc/resolv.conf", "/etc/shadow"} {
		if target == protected || strings.HasPrefix(target, protected+"/") || strings.HasPrefix(protected, target+"/") {
			return true
		}
	}
	return false
}

// CleanupResolvedSecretFiles removes the allocation-owned host files backing
// secret bind mounts. Call it only after the runtime has released those mounts.
func CleanupResolvedSecretFiles(allocationID string) error {
	if strings.TrimSpace(allocationID) == "" {
		return nil
	}
	return os.RemoveAll(resolvedSecretRoot(allocationID))
}

func resolvedSecretRoot(allocationID string) string {
	allocationKey := fmt.Sprintf("%x", sha256.Sum256([]byte(allocationID)))
	return filepath.Join(os.TempDir(), "axnoded-secrets", allocationKey)
}
