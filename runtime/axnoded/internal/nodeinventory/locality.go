package nodeinventory

import (
	"fmt"
	"path/filepath"
	"strings"

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
	case "S3":
		if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.Object == "" {
			return "", false
		}
		return fmt.Sprintf("s3:%s/%s/%s", cfg.Endpoint, cfg.Bucket, strings.TrimPrefix(cfg.Object, "/")), true
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
	case "S3":
		return "s3"
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
	case "S3":
		return "oss"
	default:
		return ""
	}
}
