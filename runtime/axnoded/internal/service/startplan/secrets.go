package startplan

import (
	"fmt"
	"path"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

// MaterializeResolvedSecretFiles projects files from private node-owned storage.
// Allocation lifecycle cleanup owns their retirement after runtime absence.
func MaterializeResolvedSecretFiles(rootDir string, request *runtime.StartRequest) ([]*runtime.Mount, func() error, error) {
	if request == nil || len(request.GetSecretFiles()) == 0 {
		return nil, nil, nil
	}
	if strings.TrimSpace(request.GetAllocationID()) == "" {
		return nil, nil, fmt.Errorf("allocation ID is required for secret files")
	}
	seenTargets := make(map[string]struct{}, len(request.GetSecretFiles()))
	for _, item := range request.GetSecretFiles() {
		if item == nil {
			return nil, nil, fmt.Errorf("resolved secret file is required")
		}
		rawTarget := strings.TrimSpace(item.Path)
		target := path.Clean(rawTarget)
		if rawTarget == "" || target == "/" || !strings.HasPrefix(target, "/") || hasParentPathElement(rawTarget) {
			return nil, nil, fmt.Errorf("resolved secret file path %q must be an absolute container path below /", rawTarget)
		}
		if protectedSecretFileTarget(target) {
			return nil, nil, fmt.Errorf("resolved secret file path %q overlaps a protected runtime path", target)
		}
		if item.GetMode() > 0o777 || item.GetMode()&0o222 != 0 {
			return nil, nil, fmt.Errorf("resolved secret file %q mode must be read-only permissions within 0777", target)
		}
		for existing := range seenTargets {
			if target == existing || strings.HasPrefix(target, existing+"/") || strings.HasPrefix(existing, target+"/") {
				return nil, nil, fmt.Errorf("resolved secret file path %q overlaps path %q", target, existing)
			}
		}
		seenTargets[target] = struct{}{}
	}
	return materializeSecretStorage(rootDir, request)
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
func CleanupResolvedSecretFiles(rootDir, allocationID string) error {
	if strings.TrimSpace(allocationID) == "" {
		return nil
	}
	return cleanupSecretStorage(rootDir, allocationID)
}
