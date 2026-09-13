package service

import (
	"errors"
	"testing"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

func TestNetworkPolicyDiagnosticStatusCategories(t *testing.T) {
	base := NetworkPolicyDiagnostics{CapabilityState: NetworkPolicyCapabilityAvailable, EnforcementHealthy: true, ExactBinding: true}
	for _, test := range []struct {
		name          string
		diagnostics   NetworkPolicyDiagnostics
		healthErr     error
		recordErr     error
		recordPresent bool
		want          NetworkPolicyStatus
	}{
		{name: "ok", diagnostics: base, recordPresent: true, want: NetworkPolicyStatusOK},
		{name: "capability unavailable", diagnostics: func() NetworkPolicyDiagnostics {
			value := base
			value.CapabilityState = NetworkPolicyCapabilityUnavailable
			return value
		}(), recordPresent: true, want: NetworkPolicyStatusCapabilityUnavailable},
		{name: "manager unavailable", diagnostics: base, healthErr: errors.New("unavailable"), recordPresent: true, want: NetworkPolicyStatusEnforcementUnhealthy},
		{name: "self test unhealthy", diagnostics: func() NetworkPolicyDiagnostics { value := base; value.EnforcementHealthy = false; return value }(), recordPresent: true, want: NetworkPolicyStatusEnforcementUnhealthy},
		{name: "record missing", diagnostics: base, recordErr: errors.New("not found"), want: NetworkPolicyStatusBindingMismatch},
		{name: "binding mismatch", diagnostics: func() NetworkPolicyDiagnostics { value := base; value.ExactBinding = false; return value }(), recordPresent: true, want: NetworkPolicyStatusBindingMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := networkPolicyDiagnosticStatus(test.diagnostics, test.healthErr, test.recordErr, test.recordPresent); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNetworkPolicyDiagnosticsDistinguishesAbsentPolicy(t *testing.T) {
	service := newTestService(t, nil)
	diagnostics := service.NetworkPolicyDiagnostics(t.Context(), "sandbox-without-policy")
	if diagnostics.Mode != NetworkPolicyModeUnrestricted || diagnostics.Status != NetworkPolicyStatusAbsent ||
		diagnostics.CapabilityState != NetworkPolicyCapabilityNotRequired || diagnostics.ExactBinding {
		t.Fatalf("absent policy diagnostics = %#v", diagnostics)
	}
}

func TestAllocationNetworkPolicyModeRecognizesDurablePolicyDependency(t *testing.T) {
	dependencies := []*capabilityv1.CapabilityDependency{{
		Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT),
	}}
	if mode := allocationNetworkPolicyMode(dependencies); mode != NetworkPolicyModeStrict {
		t.Fatalf("durable dependency mode = %q, want strict", mode)
	}
}

func TestSummarizeNetworkPolicyReturnsCountsWithoutValues(t *testing.T) {
	record := &runtimeegressv1.PreparedEgressPolicy{Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{
		AllowedDomains: []string{"private.destination.example", "*.private.destination.example"},
		AllowedCidrs:   []*commonv1.CIDREgressRule{{Ports: []*commonv1.PortRange{{Start: 443, End: 443}, {Start: 8000, End: 8002}}}},
	}}}}
	mode, domains, cidrs, ports := summarizeNetworkPolicy(record)
	if mode != NetworkPolicyModeStrict || domains != 2 || cidrs != 1 || ports != 2 {
		t.Fatalf("summary = (%q, %d, %d, %d)", mode, domains, cidrs, ports)
	}
}

func TestApplyNetworkPolicyRecordChecksExactAllocationBinding(t *testing.T) {
	record := &runtimeegressv1.PreparedEgressPolicy{
		AllocationID: "sandbox-1", SandboxIp: "192.0.2.10",
		Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{
			DeniedDomains: []string{"private.destination.example", "*.private.destination.example"},
		}}},
	}
	diagnostics := NetworkPolicyDiagnostics{}
	applyNetworkPolicyRecord(&diagnostics, record, NetworkPolicyModeDNSDeny, "sandbox-1", "192.0.2.10")
	if diagnostics.Mode != NetworkPolicyModeDNSDeny || diagnostics.DomainRuleCount != 2 || diagnostics.TotalRuleCount != 2 || !diagnostics.ExactBinding {
		t.Fatalf("binding diagnostics = %#v", diagnostics)
	}
	applyNetworkPolicyRecord(&diagnostics, record, NetworkPolicyModeDNSDeny, "sandbox-1", "192.0.2.11")
	if diagnostics.ExactBinding {
		t.Fatal("wrong source IP retained exact binding")
	}
}

func TestNetworkPolicyEnforcementHealthUsesModeSpecificSelfTest(t *testing.T) {
	health := &runtimeegressv1.EgressManagerHealth{
		Status:              runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK,
		DnsPolicySelfTestOk: true, StrictEgressSelfTestOk: false,
	}
	if !networkPolicyEnforcementHealthy(health, NetworkPolicyModeDNSDeny) {
		t.Fatal("DNS-only policy ignored its healthy self-test")
	}
	if networkPolicyEnforcementHealthy(health, NetworkPolicyModeStrict) {
		t.Fatal("strict policy accepted a failed strict self-test")
	}
}
