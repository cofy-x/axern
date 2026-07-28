package storage

import (
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func updateMaskPaths(req *storagev1.UpdateVolumeClaimRequest) map[string]struct{} {
	paths := map[string]struct{}{}
	for _, path := range req.GetUpdateMask().GetPaths() {
		path = strings.TrimSpace(path)
		if path != "" {
			paths[path] = struct{}{}
		}
	}
	if len(paths) == 0 {
		paths["requested_capacity"] = struct{}{}
		paths["labels"] = struct{}{}
	}
	return paths
}

func validateUpdateMaskPaths(paths map[string]struct{}) error {
	for path := range paths {
		switch path {
		case "requested_capacity", "labels":
		default:
			return fmt.Errorf("unsupported volume claim update path %q", path)
		}
	}
	return nil
}

func shouldUpdate(paths map[string]struct{}, path string) bool {
	_, ok := paths[path]
	return ok
}
