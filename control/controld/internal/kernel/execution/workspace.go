package executionkernel

import (
	"path"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateWorkspaceConfig(config *commonv1.ExecutionConfig) error {
	if config.GetResources().GetLimits().GetMemoryBytes() <= 0 {
		return status.Error(codes.InvalidArgument, "Workspace executions require a positive memory limit for verified process cleanup")
	}
	if config.GetRootfsSnapshot() != nil {
		return status.Error(codes.InvalidArgument, "Workspace and rootfs snapshot publication cannot be requested together")
	}
	overlaps := func(target string) bool {
		target = path.Clean(target)
		return target == "/" || target == "/workspace" || strings.HasPrefix(target, "/workspace/")
	}
	for _, mount := range config.GetImageMounts() {
		if overlaps(mount.GetTarget()) {
			return status.Error(codes.InvalidArgument, "image mount overlaps managed /workspace")
		}
	}
	for _, secret := range config.GetSecretFiles() {
		if overlaps(secret.GetPath()) {
			return status.Error(codes.InvalidArgument, "Secret projection overlaps managed /workspace")
		}
	}
	return nil
}
