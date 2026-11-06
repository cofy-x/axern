package imagefsd

import (
	"fmt"
	"os"
	"path/filepath"
)

// ValidateDisabled checks retained daemon authority without loading daemons,
// starting watchers, reconciling launch policy, or removing stale metadata.
// Every entry requires operator resolution: unknown or damaged metadata must
// not become permission to ignore a potentially live Nydus mount.
func ValidateDisabled(root string) error {
	dir := filepath.Join(root, "daemon_configs")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect retained daemon metadata: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("retained daemon metadata path %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read retained daemon metadata: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("retained daemon state %s requires Nydus enabled or explicit operator resolution", filepath.Join(dir, entries[0].Name()))
	}
	return nil
}
