package allocation

import (
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func TestExecutionProfileFromProtoPreservesDefaultsForPartialProfile(t *testing.T) {
	got := ExecutionProfileFromProto(&environmentv1.OciExecutionProfile{
		Baseline: &environmentv1.OciBaselinePolicy{
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
}
