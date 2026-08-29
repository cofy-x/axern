package enforcement

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

func TestDomainMatcherKeepsWildcardOffApex(t *testing.T) {
	if domainMatches("*.example.com", "example.com") {
		t.Fatal("wildcard matched apex")
	}
	for _, name := range []string{"a.example.com", "a.b.example.com"} {
		if !domainMatches("*.example.com", name) {
			t.Fatalf("wildcard did not match %q", name)
		}
	}
}

func TestAuthorizationExpiresAndIsBoundToDomain(t *testing.T) {
	engine := NewEngine()
	engine.auth["10.0.0.2"] = map[netip.Addr][]authorization{
		netip.MustParseAddr("192.0.2.10"): {{domain: "allowed.example", expiry: time.Now().Add(time.Minute)}},
	}
	if !engine.authorized("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("valid authorization rejected")
	}
	if engine.authorized("10.0.0.2", "other.example", netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("shared-IP virtual host escaped authorization")
	}
	engine.auth["10.0.0.2"][netip.MustParseAddr("192.0.2.11")] = []authorization{{domain: "allowed.example", expiry: time.Now().Add(-time.Second)}}
	if engine.authorized("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.11")) {
		t.Fatal("expired authorization accepted")
	}
}

func TestPolicyAttemptChangeClearsIPAuthorization(t *testing.T) {
	engine := NewEngine()
	first := &runtimeegressv1.PreparedEgressPolicy{AllocationID: "old", Attempt: 1, SandboxIp: "10.0.0.2", PolicyDigest: "sha256:old", Policy: strictPolicy("allowed.example")}
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{first})
	engine.authorize("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.10"), 60)
	second := &runtimeegressv1.PreparedEgressPolicy{AllocationID: "new", Attempt: 2, SandboxIp: "10.0.0.2", PolicyDigest: "sha256:new", Policy: strictPolicy("allowed.example")}
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{second})
	if engine.authorized("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("IP reuse inherited stale DNS authorization")
	}
}

func TestRenderNFTSeparatesDNSOnlyAndStrictPolicies(t *testing.T) {
	records := []*runtimeegressv1.PreparedEgressPolicy{
		{SandboxIp: "10.0.0.2", Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"blocked.example"}}}}},
		{SandboxIp: "10.0.0.3", Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"allowed.example"}, AllowedCidrs: []*commonv1.CIDREgressRule{{Cidr: "192.0.2.0/24", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 22, End: 22}}}}}}}},
	}
	wire, err := RenderNFT(records)
	if err != nil {
		t.Fatalf("RenderNFT() error = %v", err)
	}
	script := string(wire)
	for _, expected := range []string{"10.0.0.2 udp dport 53", "10.0.0.3 tcp dport 443", "192.0.2.0/24 tcp dport 22 accept", "10.0.0.3 drop", "meta mark set 0xa6e1 tproxy ip to 0.0.0.0:1080 accept", "10.0.0.3 tcp dport 80 drop"} {
		if !strings.Contains(script, expected) {
			t.Fatalf("nft script missing %q:\n%s", expected, script)
		}
	}
	if strings.Contains(script, "10.0.0.2 drop") {
		t.Fatalf("DNS-only policy became strict:\n%s", script)
	}
	if !strings.Contains(script, "ip saddr 10.0.0.3 drop") || !strings.Contains(script, "ip saddr 10.0.0.3 counter drop") {
		t.Fatalf("strict policy must gate forwarded and host-local traffic after proxy interception:\n%s", script)
	}
}
