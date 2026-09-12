package nodeinventory

import (
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
)

func LocalityKeyFromRootfsConfig(cfg langruntime.RootfsConfig) (string, bool) {
	switch cfg.SrcType.String() {
	case "LOCAL":
		if cfg.Path == "" {
			return "", false
		}
		return "local:" + filepath.Clean(cfg.Path), true
	case "IMAGE":
		if cfg.ImageUrl == "" {
			return "", false
		}
		if cfg.ImageCacheKey != "" {
			return "image:" + cfg.ImageCacheKey, true
		}
		return "image:" + cfg.ImageUrl, true
	default:
		return "", false
	}
}

func RootfsTypeFromConfig(cfg langruntime.RootfsConfig) string {
	switch cfg.SrcType.String() {
	case "LOCAL":
		return "local"
	case "IMAGE":
		return "image"
	default:
		return "unknown"
	}
}

func MountTypeFromConfig(cfg langruntime.RootfsConfig) string {
	switch cfg.SrcType.String() {
	case "LOCAL":
		return "local"
	case "IMAGE":
		return "oci"
	default:
		return ""
	}
}
