package executionkernel

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWorkspaceRejectsOverlappingInputsAndRootfsPublication(t *testing.T) {
	resources := &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 1 << 20}}
	if err := ValidateWorkspaceConfig(&commonv1.ExecutionConfig{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing cleanup identity accepted: %v", err)
	}
	for _, target := range []string{"/", "/workspace", "/workspace/config/key"} {
		for _, config := range []*commonv1.ExecutionConfig{{ImageMounts: []*commonv1.ImageMount{{Target: target}}}, {SecretFiles: []*commonv1.SecretFile{{Path: target}}}} {
			config.Resources = resources
			if err := ValidateWorkspaceConfig(config); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("overlapping target %q accepted: %v", target, err)
			}
		}
	}
	if err := ValidateWorkspaceConfig(&commonv1.ExecutionConfig{Resources: resources, RootfsSnapshot: &commonv1.RootfsSnapshot{}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("rootfs publication accepted: %v", err)
	}
	if err := ValidateWorkspaceConfig(&commonv1.ExecutionConfig{Resources: resources, ImageMounts: []*commonv1.ImageMount{{Target: "/reference"}}, SecretFiles: []*commonv1.SecretFile{{Path: "/run/key"}}}); err != nil {
		t.Fatal(err)
	}
}
