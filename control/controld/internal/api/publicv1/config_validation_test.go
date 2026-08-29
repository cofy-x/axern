package publicv1

import (
	"testing"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestValidateExecutionConfigSecretRefs(t *testing.T) {
	tests := []struct {
		name     string
		config   *commonv1.ExecutionConfig
		wantCode codes.Code
	}{
		{
			name: "duplicate env name",
			config: &commonv1.ExecutionConfig{SecretEnv: []*commonv1.SecretEnvVar{
				{Name: "TOKEN", SecretID: "sec-1", Key: "token"},
				{Name: "TOKEN", SecretID: "sec-2", Key: "token"},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "duplicate file path",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/a", SecretID: "sec-1", Key: "a"},
				{Path: "/a", SecretID: "sec-2", Key: "b"},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "missing secret id",
			config: &commonv1.ExecutionConfig{SecretEnv: []*commonv1.SecretEnvVar{
				{Name: "TOKEN", Key: "token"},
			}},
			wantCode: codes.InvalidArgument,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExecutionConfigSecretRefs(tc.config)
			if grpcstatus.Code(err) != tc.wantCode {
				t.Fatalf("code = %v, want %v", grpcstatus.Code(err), tc.wantCode)
			}
		})
	}
}

func TestValidateServiceVolumeMounts(t *testing.T) {
	valid := &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
		Name:    "data_1",
		Target:  "/var/lib/app",
		Options: []string{"rbind", "nodev"},
	}}}
	if err := validateServiceVolumeMounts(valid); err != nil {
		t.Fatalf("validateServiceVolumeMounts(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		config *commonv1.ExecutionConfig
	}{
		{
			name: "invalid name",
			config: &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name: "bad/name", Target: "/data",
			}}},
		},
		{
			name: "root target",
			config: &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name: "data", Target: "/",
			}}},
		},
		{
			name: "parent target",
			config: &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name: "data", Target: "/var/../data",
			}}},
		},
		{
			name: "duplicate target",
			config: &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{
				{Name: "a", Target: "/data"},
				{Name: "b", Target: "/data"},
			}},
		},
		{
			name: "unsupported option",
			config: &commonv1.ExecutionConfig{VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name: "data", Target: "/data", Options: []string{"shared"},
			}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateServiceVolumeMounts(tc.config); grpcstatus.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (err=%v)", grpcstatus.Code(err), err)
			}
		})
	}
}

func TestValidateExecutionConfigImageMounts(t *testing.T) {
	workspace := &commonv1.WorkspaceImageSource{
		Variants:   []*commonv1.WorkspaceImageVariant{{Format: "nydus", Image: "example.com/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Format: "oci", Image: "example.com/task@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		SourcePath: "tasks/task-a/workspace", Target: "/workspace",
	}
	valid := &commonv1.ExecutionConfig{WorkspaceImage: workspace, ImageMounts: []*commonv1.ImageMount{{
		Image: "example.com/tools/codex:latest", Target: "/opt/axern/tools/codex",
	}}}
	if err := validateExecutionConfigImageMounts(valid); err != nil {
		t.Fatalf("validateExecutionConfigImageMounts(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		config *commonv1.ExecutionConfig
	}{
		{
			name: "duplicate workspace format",
			config: &commonv1.ExecutionConfig{WorkspaceImage: &commonv1.WorkspaceImageSource{
				Variants:   []*commonv1.WorkspaceImageVariant{{Format: "oci", Image: "example.com/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Format: "oci", Image: "example.com/task@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
				SourcePath: "tasks/task-a/workspace", Target: "/workspace",
			}},
		},
		{
			name: "ambiguous workspace source",
			config: &commonv1.ExecutionConfig{WorkspaceImage: &commonv1.WorkspaceImageSource{
				Variants:   []*commonv1.WorkspaceImageVariant{{Format: "oci", Image: "example.com/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
				SourcePath: "tasks/group/task-a/workspace", Target: "/workspace",
			}},
		},
		{
			name: "workspace overlaps volume",
			config: &commonv1.ExecutionConfig{WorkspaceImage: workspace, VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name: "data", Target: "/workspace/data",
			}}},
		},
		{
			name: "missing image",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Target: "/opt/axern/tools/codex",
			}}},
		},
		{
			name: "root target",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Image: "image", Target: "/",
			}}},
		},
		{
			name: "protected target",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Image: "image", Target: "/usr",
			}}},
		},
		{
			name: "parent target",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Image: "image", Target: "/opt/../tools",
			}}},
		},
		{
			name: "overlapping image mounts",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{
				{Image: "image-a", Target: "/opt/axern/tools"},
				{Image: "image-b", Target: "/opt/axern/tools/codex"},
			}},
		},
		{
			name: "Claude public alias overlaps image mount",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{
				{Image: "claude", Target: "/__claude_code"},
				{Image: "other", Target: "/opt/axern/agents/claude-code"},
			}},
		},
		{
			name: "Claude public alias overlaps workspace image",
			config: &commonv1.ExecutionConfig{
				WorkspaceImage: &commonv1.WorkspaceImageSource{
					Variants:   []*commonv1.WorkspaceImageVariant{{Format: "oci", Image: "example.com/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
					SourcePath: "tasks/task-a/workspace", Target: "/opt/axern/agents/claude-code/workspace",
				},
				ImageMounts: []*commonv1.ImageMount{{Image: "claude", Target: "/__claude_code"}},
			},
		},
		{
			name: "Claude public alias overlaps volume mount",
			config: &commonv1.ExecutionConfig{
				ImageMounts:  []*commonv1.ImageMount{{Image: "claude", Target: "/__claude_code"}},
				VolumeMounts: []*commonv1.ServiceVolumeMount{{Name: "data", Target: "/opt/axern/agents/claude-code/data"}},
			},
		},
		{
			name: "Claude public alias overlaps secret file",
			config: &commonv1.ExecutionConfig{
				ImageMounts: []*commonv1.ImageMount{{Image: "claude", Target: "/__claude_code"}},
				SecretFiles: []*commonv1.SecretFile{{Path: "/opt/axern/agents/claude-code/token", SecretID: "sec", Key: "token"}},
			},
		},
		{
			name: "overlapping volume mount",
			config: &commonv1.ExecutionConfig{
				ImageMounts: []*commonv1.ImageMount{{Image: "image", Target: "/opt/axern/tools"}},
				VolumeMounts: []*commonv1.ServiceVolumeMount{{
					Name: "data", Target: "/opt/axern/tools/data",
				}},
			},
		},
		{
			name: "overlapping secret file",
			config: &commonv1.ExecutionConfig{
				ImageMounts: []*commonv1.ImageMount{{Image: "image", Target: "/opt/axern/tools"}},
				SecretFiles: []*commonv1.SecretFile{{
					Path: "/opt/axern/tools/token", SecretID: "sec", Key: "token",
				}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateExecutionConfigImageMounts(tc.config); grpcstatus.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (err=%v)", grpcstatus.Code(err), err)
			}
		})
	}
}

func TestValidateExecutionConfigCapabilitiesRejectsMalformedRequirements(t *testing.T) {
	for _, config := range []*commonv1.ExecutionConfig{
		{ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{nil}},
		{ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{}}},
		{ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{Capability: &capabilityv1.ExtensionCapability{Name: " example.com/accelerator"}}}},
	} {
		if err := validateExecutionConfigCapabilities(config); grpcstatus.Code(err) != codes.InvalidArgument {
			t.Fatalf("validateExecutionConfigCapabilities(%+v) code = %s, want InvalidArgument", config, grpcstatus.Code(err))
		}
	}
}

func TestValidateExecutionConfigNetworkRejectsUnsafeOrAmbiguousPolicy(t *testing.T) {
	tests := []*commonv1.ExecutionConfig{
		{Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}}},
		{Network: &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"https://example.com"}}}}}},
		{Network: &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: []*commonv1.CIDREgressRule{{Cidr: "169.254.169.254/32", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 80, End: 80}}}}}}}}},
	}
	for _, config := range tests {
		if err := validateExecutionConfigNetwork(config); grpcstatus.Code(err) != codes.InvalidArgument {
			t.Fatalf("validateExecutionConfigNetwork(%+v) code = %s, want InvalidArgument", config, grpcstatus.Code(err))
		}
	}
}

func TestValidateNoServiceVolumeMounts(t *testing.T) {
	err := validateNoServiceVolumeMounts(&commonv1.ExecutionConfig{
		VolumeMounts: []*commonv1.ServiceVolumeMount{{Name: "data", Target: "/data"}},
	}, "run")
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
}

func TestValidateOptionalExecutionArgv(t *testing.T) {
	for _, config := range []*commonv1.ExecutionConfig{
		nil,
		{},
		{Argv: []string{"python", "-c", "print('ok')"}},
		{Argv: []string{"python", ""}},
	} {
		if err := validateOptionalExecutionArgv(config); err != nil {
			t.Fatalf("validateOptionalExecutionArgv(%+v) error = %v", config, err)
		}
	}

	if err := validateOptionalExecutionArgv(&commonv1.ExecutionConfig{Argv: []string{"", "python"}}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err=%v)", grpcstatus.Code(err), err)
	}
}
