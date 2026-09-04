package main

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestConfigValidationRequiresMatchingHermeticFixtureFamily(t *testing.T) {
	cfg := config{
		runtimeName: "runc", networkBackend: "bridge", ipFamily: "ipv4", policyMode: "strict_domain",
		samples: 2, concurrency: 2, payloadBytes: 1024, sustainedSeconds: 1, ruleScaleCounts: []uint32{1}, output: "/tmp/result.json",
		fixtureAddress: "192.0.2.10", dnsServer: "192.0.2.53", operationTimeout: time.Second, startupTimeout: time.Second,
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	cfg.dnsServer = "2001:db8::53"
	if err := cfg.validate(); err == nil {
		t.Fatal("mixed address families were accepted")
	}
}

func TestPolicyForDefinesOnlyModeSpecificGrants(t *testing.T) {
	if policy := policyFor("unrestricted", "192.0.2.10"); policy != nil {
		t.Fatalf("unrestricted policy = %#v, want nil", policy)
	}
	dns := policyFor("dns_deny", "192.0.2.10").GetDnsDeny()
	if len(dns.GetDeniedDomains()) != 1 || dns.GetDeniedDomains()[0] != deniedFixtureDomain {
		t.Fatalf("unexpected DNS deny policy: %#v", dns)
	}
	domain := policyFor("strict_domain", "192.0.2.10").GetStrict()
	if len(domain.GetAllowedDomains()) != 1 || len(domain.GetAllowedCidrs()) != 0 {
		t.Fatalf("unexpected strict domain policy: %#v", domain)
	}
	cidr := policyFor("strict_cidr", "2001:db8::10").GetStrict()
	if len(cidr.GetAllowedCidrs()) != 2 || cidr.GetAllowedCidrs()[0].GetCidr() != "2001:db8::10/128" || cidr.GetAllowedCidrs()[0].GetProtocol() != commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP || cidr.GetAllowedCidrs()[1].GetProtocol() != commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP {
		t.Fatalf("unexpected strict CIDR policy: %#v", cidr)
	}
}

func TestDistributionUsesNearestRankQuantiles(t *testing.T) {
	distribution := makeDistribution([]float64{10, 1, 8, 2, 6, 3, 9, 4, 7, 5})
	if distribution.Samples != 10 || distribution.P50 != 5 || distribution.P95 != 10 || distribution.P99 != 10 || distribution.Max != 10 {
		t.Fatalf("unexpected distribution: %#v", distribution)
	}
}

func TestScalePolicyProducesRequestedUniqueRules(t *testing.T) {
	policy := scalePolicy(256, "ipv4").GetStrict()
	if len(policy.GetAllowedCidrs()) != 256 {
		t.Fatalf("scale policy rules = %d, want 256", len(policy.GetAllowedCidrs()))
	}
	seen := map[string]struct{}{}
	for _, rule := range policy.GetAllowedCidrs() {
		if _, exists := seen[rule.GetCidr()]; exists {
			t.Fatalf("duplicate scale CIDR %q", rule.GetCidr())
		}
		seen[rule.GetCidr()] = struct{}{}
	}
	if got := scalePolicy(1, "ipv6").GetStrict().GetAllowedCidrs()[0].GetCidr(); got != "2001:db8:100:0::1/128" {
		t.Fatalf("IPv6 scale CIDR = %q", got)
	}
}
