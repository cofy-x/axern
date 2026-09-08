package verifyutil

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestPolicyCapabilityRequirements(t *testing.T) {
	tests := []struct {
		name       string
		network    *commonv1.NetworkSpec
		wantDNS    bool
		wantStrict bool
	}{
		{name: "unrestricted"},
		{
			name: "dns deny",
			network: &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{
				DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"blocked.test"}},
			}}},
			wantDNS: true,
		},
		{
			name: "strict domain",
			network: &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"allowed.test"}},
			}}},
			wantStrict: true,
		},
		{
			name: "isolated deny all does not require egressd proof",
			network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{},
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dns, strict := policyCapabilityRequirements(test.network)
			if dns != test.wantDNS || strict != test.wantStrict {
				t.Fatalf("policyCapabilityRequirements() = (%t, %t), want (%t, %t)", dns, strict, test.wantDNS, test.wantStrict)
			}
		})
	}
}
