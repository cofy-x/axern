package sandbox

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
)

func TestRenderNetworkPolicyJSONUsesStableBoundedFields(t *testing.T) {
	response := &nodeoperatorv1.ExplainSandboxNetworkPolicyResponse{
		SandboxID: "sandbox-1", Mode: nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_STRICT,
		Status:             nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK,
		CapabilityState:    nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_AVAILABLE,
		EnforcementHealthy: true, ExactProof: true, AllocationAttempt: 2, ExecutionRevision: 7, EnforcementRevision: 11,
		DomainRuleCount: 3, CidrRuleCount: 2, PortRangeCount: 4, TotalRuleCount: 5, RecoveredAfterRestart: true,
	}
	var output bytes.Buffer
	if err := renderNetworkPolicyJSON(&output, response); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["mode"] != "strict" || fields["status"] != "ok" || fields["capability_state"] != "available" {
		t.Fatalf("unexpected JSON: %s", output.String())
	}
	if len(fields) != 14 {
		t.Fatalf("JSON field count = %d, want 14: %s", len(fields), output.String())
	}
	for name := range fields {
		for _, forbidden := range []string{"domain_name", "host", "sni", "remote_ip", "cidr_value", "policy_digest", "raw"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("privacy-sensitive field %q entered JSON diagnostics", name)
			}
		}
	}
}

func TestNetworkPolicyDoctorTreatsAbsentAsHealthyAndFailuresAsDegraded(t *testing.T) {
	for _, status := range []nodeoperatorv1.SandboxNetworkPolicyStatus{
		nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK,
		nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ABSENT,
	} {
		if !networkPolicyDoctorHealthy(status) {
			t.Fatalf("status %v should be healthy", status)
		}
	}
	if networkPolicyDoctorHealthy(nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_PROOF_STALE) {
		t.Fatal("stale proof passed doctor")
	}
}

func TestNetworkPolicyRenderingBoundsUnknownEnumsAndRejectsNil(t *testing.T) {
	if networkPolicyMode(99) != "unspecified" || networkPolicyStatus(99) != "unspecified" || networkPolicyCapabilityState(99) != "unspecified" {
		t.Fatal("unknown enum escaped the stable bounded vocabulary")
	}
	if err := renderNetworkPolicyJSON(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("nil diagnostics response rendered successfully")
	}
}
