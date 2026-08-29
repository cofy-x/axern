package axernsdk

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestDenyDNSNetworkPolicyNormalizesDomains(t *testing.T) {
	policy, err := DenyDNSNetworkPolicy("GitHub.COM.", "github.com", "*.BÜCHER.example")
	if err != nil {
		t.Fatalf("DenyDNSNetworkPolicy() error = %v", err)
	}
	got := policy.proto().GetDnsDeny().GetDeniedDomains()
	if len(got) != 2 || got[0] != "github.com" || got[1] != "*.xn--bcher-kva.example" {
		t.Fatalf("domains = %v", got)
	}
}

func TestNewStrictNetworkPolicyBuildsCIDRRules(t *testing.T) {
	policy, err := NewStrictNetworkPolicy([]string{"example.com"}, []CIDRRule{{CIDR: "192.0.2.7/24", Protocol: EgressProtocolTCP, Ports: []PortRange{{Start: 22}, {Start: 8000, End: 8002}}}})
	if err != nil {
		t.Fatalf("NewStrictNetworkPolicy() error = %v", err)
	}
	rule := policy.proto().GetStrict().GetAllowedCidrs()[0]
	if rule.GetCidr() != "192.0.2.0/24" || rule.GetProtocol() != commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP || rule.GetPorts()[1].GetEnd() != 8002 {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestDenyAllNetworkPolicyIsStrictEmpty(t *testing.T) {
	strict := DenyAllNetworkPolicy().proto().GetStrict()
	if strict == nil || len(strict.GetAllowedDomains()) != 0 || len(strict.GetAllowedCidrs()) != 0 {
		t.Fatalf("strict = %+v", strict)
	}
}

func TestNetworkPolicyRejectsInvalidRules(t *testing.T) {
	for _, domain := range []string{"https://example.com", "example.com:443", "127.0.0.1", "foo.*.example"} {
		if _, err := AllowDomainNetworkPolicy(domain); err == nil {
			t.Fatalf("AllowDomainNetworkPolicy(%q) succeeded", domain)
		}
	}
	if _, err := NewStrictNetworkPolicy(nil, []CIDRRule{{CIDR: "192.0.2.0/24", Protocol: EgressProtocolTCP, Ports: []PortRange{{Start: 0}}}}); err == nil {
		t.Fatal("invalid port succeeded")
	}
}
