package allocation

import (
	"testing"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/protobuf/proto"
)

func TestExecutionProfileFromProtoPreservesDefaultsForPartialProfile(t *testing.T) {
	got := ExecutionProfileFromProto(&catalogv1.OciExecutionProfile{
		Baseline: &catalogv1.OciBaselinePolicy{
			Capabilities: []string{"CAP_SYS_PTRACE"},
			NoFileLimit:  2097152,
		},
	})
	if got == nil {
		t.Fatal("ExecutionProfileFromProto() = nil, want profile")
	}
	if got.Baseline.NoFileLimit != 2097152 {
		t.Fatalf("nofile limit = %d, want 2097152", got.Baseline.NoFileLimit)
	}
	if len(got.Baseline.Capabilities) != 1 || got.Baseline.Capabilities[0] != "CAP_SYS_PTRACE" {
		t.Fatalf("baseline capabilities = %#v, want CAP_SYS_PTRACE only", got.Baseline.Capabilities)
	}
	if got.Capabilities.AnnotationKey != "linux-capabilities" {
		t.Fatalf("capability annotation = %q, want linux-capabilities", got.Capabilities.AnnotationKey)
	}
	if !got.Capabilities.IncludeAmbient {
		t.Fatal("include ambient = false, want default true")
	}
}

func TestExecutionProfileFromProtoHonorsCapabilityAmbientOverride(t *testing.T) {
	got := ExecutionProfileFromProto(&catalogv1.OciExecutionProfile{
		Capabilities: &catalogv1.OciCapabilityPolicy{
			AnnotationKey:  "custom-capabilities",
			IncludeAmbient: proto.Bool(false),
		},
	})
	if got == nil {
		t.Fatal("ExecutionProfileFromProto() = nil, want profile")
	}
	if got.Capabilities.AnnotationKey != "custom-capabilities" {
		t.Fatalf("capability annotation = %q, want custom-capabilities", got.Capabilities.AnnotationKey)
	}
	if got.Capabilities.IncludeAmbient {
		t.Fatal("include ambient = true, want false")
	}
	if got.Baseline.NoFileLimit != 1048576 {
		t.Fatalf("nofile limit = %d, want default 1048576", got.Baseline.NoFileLimit)
	}
}
