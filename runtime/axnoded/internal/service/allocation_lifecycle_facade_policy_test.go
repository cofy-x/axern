package service

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestRequirementInputIncludesEgressPolicyCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		policy     *commonv1.NetworkEgressPolicy
		wantDNS    bool
		wantStrict bool
	}{
		{name: "none"},
		{
			name: "dns deny",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{
				DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"blocked.test"}},
			}},
			wantDNS: true,
		},
		{
			name: "strict domain",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"allowed.test"}},
			}},
			wantStrict: true,
		},
		{
			name: "strict cidr",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: []*commonv1.CIDREgressRule{{Cidr: "198.19.0.0/24"}}},
			}},
			wantStrict: true,
		},
		{
			name: "strict deny all",
			policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{},
			}},
		},
	}

	service := &sandboxService{config: config.DefaultConfig()}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := service.requirementInput(&runtime.StartRequest{EgressPolicy: test.policy}, false)
			if input.RequiresDNSPolicyEnforcement != test.wantDNS || input.RequiresStrictEgressEnforcement != test.wantStrict {
				t.Fatalf("policy requirements = (dns=%t, strict=%t), want (dns=%t, strict=%t)",
					input.RequiresDNSPolicyEnforcement, input.RequiresStrictEgressEnforcement, test.wantDNS, test.wantStrict)
			}
		})
	}
}
