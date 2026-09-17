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
			name: "overlapping file paths",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/run/secrets", SecretID: "sec-1", Key: "a"},
				{Path: "/run/secrets/token", SecretID: "sec-2", Key: "b"},
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
		{
			name: "invalid env name",
			config: &commonv1.ExecutionConfig{SecretEnv: []*commonv1.SecretEnvVar{
				{Name: "MODEL-TOKEN", SecretID: "sec-1", Key: "token"},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "relative file path",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "run/secrets/token", SecretID: "sec-1", Key: "token"},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "parent file path",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/run/secrets/../token", SecretID: "sec-1", Key: "token"},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "unsafe file mode",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/run/secrets/token", SecretID: "sec-1", Key: "token", Mode: 0o1000},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "writable file mode",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/run/secrets/token", SecretID: "sec-1", Key: "token", Mode: 0o600},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "protected runtime path",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/proc/sys/kernel/token", SecretID: "sec-1", Key: "token", Mode: 0o400},
			}},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "critical system file",
			config: &commonv1.ExecutionConfig{SecretFiles: []*commonv1.SecretFile{
				{Path: "/etc/shadow", SecretID: "sec-1", Key: "token", Mode: 0o400},
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

func TestValidateExecutionConfigImageMounts(t *testing.T) {
	valid := &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
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
			name: "target below protected path",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Image: "example.com/tool:latest", Target: "/usr/local/tool",
			}}},
		},
		{
			name: "invalid OCI reference",
			config: &commonv1.ExecutionConfig{ImageMounts: []*commonv1.ImageMount{{
				Image: "not a valid image reference", Target: "/__tool",
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
