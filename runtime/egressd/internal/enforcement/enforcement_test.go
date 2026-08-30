package enforcement

import (
	"fmt"
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
	now := time.Unix(1_000, 0)
	engine.now = func() time.Time { return now }
	engine.auth["10.0.0.2"] = map[netip.Addr][]authorization{
		netip.MustParseAddr("192.0.2.10"): {{domain: "allowed.example", expiry: now.Add(time.Minute)}},
	}
	if !engine.authorized("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("valid authorization rejected")
	}
	if engine.authorized("10.0.0.2", "other.example", netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("shared-IP virtual host escaped authorization")
	}
	engine.auth["10.0.0.2"][netip.MustParseAddr("192.0.2.11")] = []authorization{{domain: "allowed.example", expiry: now.Add(-time.Second)}}
	if engine.authorized("10.0.0.2", "allowed.example", netip.MustParseAddr("192.0.2.11")) {
		t.Fatal("expired authorization accepted")
	}
}

func TestAuthorizationIsDeduplicatedBoundedAndExpiresAtConnect(t *testing.T) {
	engine := NewEngine()
	now := time.Unix(1_000, 0)
	engine.now = func() time.Time { return now }
	address := netip.MustParseAddr("192.0.2.10")
	for range 10_000 {
		engine.authorize("10.0.0.2", "allowed.example", address, 30)
	}
	if got := len(engine.auth["10.0.0.2"][address]); got != 1 {
		t.Fatalf("duplicate authorizations = %d, want 1", got)
	}
	for index := range maxAuthorizationsPerAddress + 10 {
		engine.authorize("10.0.0.2", fmt.Sprintf("host-%d.example", index), address, 30)
	}
	if got := len(engine.auth["10.0.0.2"][address]); got != maxAuthorizationsPerAddress {
		t.Fatalf("authorizations per address = %d, want %d", got, maxAuthorizationsPerAddress)
	}
	if !engine.authorized("10.0.0.2", "allowed.example", address) {
		t.Fatal("authorization rejected before TTL boundary")
	}
	now = now.Add(30 * time.Second)
	if engine.authorized("10.0.0.2", "allowed.example", address) {
		t.Fatal("authorization accepted at TTL boundary")
	}
	if _, ok := engine.auth["10.0.0.2"]; ok {
		t.Fatal("expired authorization state was not reclaimed")
	}
}

func TestAuthorizationAddressSetIsBounded(t *testing.T) {
	engine := NewEngine()
	for index := range maxAuthorizationAddressesPerSource + 10 {
		address := netip.AddrFrom4([4]byte{192, 0, byte(index >> 8), byte(index)})
		engine.authorize("10.0.0.2", "allowed.example", address, 30)
	}
	if got := len(engine.auth["10.0.0.2"]); got != maxAuthorizationAddressesPerSource {
		t.Fatalf("authorization addresses = %d, want %d", got, maxAuthorizationAddressesPerSource)
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
	for _, expected := range []string{"10.0.0.2 udp dport 53", "10.0.0.3 tcp dport 443", "192.0.2.0/24 tcp dport 22 accept", "10.0.0.3 drop", "tproxy ip to :1080 meta mark set 0xa6e1 accept", "10.0.0.3 tcp dport 80 drop"} {
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

func TestRenderNFTDoesNotProxyDNSForStrictPoliciesWithoutDomains(t *testing.T) {
	records := []*runtimeegressv1.PreparedEgressPolicy{
		{SandboxIp: "10.0.0.4", Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}},
		{SandboxIp: "10.0.0.5", Policy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: []*commonv1.CIDREgressRule{{Cidr: "10.10.0.0/16", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP, Ports: []*commonv1.PortRange{{Start: 53, End: 53}}}}}}}},
	}
	wire, err := RenderNFT(records)
	if err != nil {
		t.Fatal(err)
	}
	script := string(wire)
	for _, source := range []string{"10.0.0.4", "10.0.0.5"} {
		if strings.Contains(script, source+" udp dport 53 meta mark set") || strings.Contains(script, source+" tcp dport 53 meta mark set") {
			t.Fatalf("policy without domains was sent to DNS proxy:\n%s", script)
		}
	}
	if !strings.Contains(script, "10.10.0.0/16 udp dport 53 accept") {
		t.Fatalf("explicit CIDR DNS rule was not preserved:\n%s", script)
	}
}

func TestRenderNFTKeepsIPv4AndIPv6FragmentsFailClosed(t *testing.T) {
	wire, err := RenderNFT([]*runtimeegressv1.PreparedEgressPolicy{
		{SandboxIp: "10.0.0.4", Policy: strictPolicy("allowed.example")},
		{SandboxIp: "fd00::4", Policy: strictPolicy("allowed.example")},
	})
	if err != nil {
		t.Fatal(err)
	}
	script := string(wire)
	for _, sourceDrop := range []string{
		"ip saddr 10.0.0.4 drop",
		"ip saddr 10.0.0.4 counter drop",
		"ip6 saddr fd00::4 drop",
		"ip6 saddr fd00::4 counter drop",
	} {
		if !strings.Contains(script, sourceDrop) {
			t.Fatalf("strict terminal drop %q is missing; non-initial fragments could escape:\n%s", sourceDrop, script)
		}
	}
}

func TestNFTManagedProofDetectsMissingOrInjectedRules(t *testing.T) {
	wire, err := RenderNFT([]*runtimeegressv1.PreparedEgressPolicy{{
		SandboxIp: "10.0.0.4", Policy: strictPolicy("allowed.example"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	expected := nftManagedProof(wire)
	if len(expected) == 0 {
		t.Fatal("rendered rules have no managed proof")
	}
	listed := append([]byte("table inet axern_egress {\n"), wire...)
	if !equalStrings(expected, nftManagedProof(listed)) {
		t.Fatal("equivalent listed rules did not preserve proof")
	}
	missing := strings.Replace(string(listed), ` comment "`+expected[0]+`"`, "", 1)
	if equalStrings(expected, nftManagedProof([]byte(missing))) {
		t.Fatal("missing managed rule was not detected")
	}
	injected := append(listed, []byte(` comment "axern:0000000000000000"`)...)
	if equalStrings(expected, nftManagedProof(injected)) {
		t.Fatal("injected managed rule was not detected")
	}
}
