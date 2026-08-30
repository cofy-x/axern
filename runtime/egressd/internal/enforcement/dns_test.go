package enforcement

import (
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSDenyReturnsRefusedWithoutConsultingUpstream(t *testing.T) {
	engine := NewEngine()
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{SandboxIp: "10.0.0.2", Policy: dnsDenyPolicy("*.blocked.example")}})
	query := dnsQuery(t, "deep.blocked.example.", dnsmessage.TypeA)
	responseWire, err := engine.resolveDNS("10.0.0.2", query, false)
	if err != nil {
		t.Fatalf("resolveDNS() error = %v", err)
	}
	var response dnsmessage.Message
	if err := response.Unpack(responseWire); err != nil {
		t.Fatal(err)
	}
	if response.Header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("rcode = %v, want REFUSED", response.Header.RCode)
	}
}

func TestStrictDNSAuthorizesReturnedIPForResponseTTL(t *testing.T) {
	questionName := dnsName(t, "allowed.example.")
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: questionName, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 10}}}}}
	})
	engine := NewEngine()
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{SandboxIp: "10.0.0.2", Policy: strictPolicy("allowed.example"), UpstreamNameservers: []string{upstream}}})
	if _, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "allowed.example.", dnsmessage.TypeA), false); err != nil {
		t.Fatalf("resolveDNS() error = %v", err)
	}
	if !engine.authorized("10.0.0.2", "allowed.example", mustAddr("192.0.2.10")) {
		t.Fatal("DNS answer did not create TTL authorization")
	}
	if engine.authorized("10.0.0.2", "other.example", mustAddr("192.0.2.10")) {
		t.Fatal("shared-IP host was authorized")
	}
}

func TestStrictDNSAuthorizesOnlyAddressesReachableThroughBoundedCNAMEChain(t *testing.T) {
	allowed := dnsName(t, "allowed.example.")
	cdn := dnsName(t, "edge.cdn.example.")
	unrelated := dnsName(t, "unrelated.example.")
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{
			Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions,
			Answers: []dnsmessage.Resource{
				{Header: dnsmessage.ResourceHeader{Name: allowed, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.CNAMEResource{CNAME: cdn}},
				{Header: dnsmessage.ResourceHeader{Name: cdn, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 20}}},
				{Header: dnsmessage.ResourceHeader{Name: unrelated, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 21}}},
			},
		}
	})
	engine := NewEngine()
	now := time.Unix(1_000, 0)
	engine.now = func() time.Time { return now }
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{
		SandboxIp: "10.0.0.2", Policy: strictPolicy("allowed.example", "unrelated.example"), UpstreamNameservers: []string{upstream},
	}})
	if _, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "allowed.example.", dnsmessage.TypeA), false); err != nil {
		t.Fatal(err)
	}
	if !engine.authorized("10.0.0.2", "allowed.example", mustAddr("192.0.2.20")) {
		t.Fatal("reachable CNAME address was not authorized for the requested domain")
	}
	now = now.Add(30 * time.Second)
	if engine.authorized("10.0.0.2", "allowed.example", mustAddr("192.0.2.20")) {
		t.Fatal("CNAME authorization outlived the shortest chain TTL")
	}
	if engine.authorized("10.0.0.2", "unrelated.example", mustAddr("192.0.2.21")) ||
		engine.authorized("10.0.0.2", "allowed.example", mustAddr("192.0.2.21")) {
		t.Fatal("unrelated address answer was authorized")
	}
}

func TestStrictDNSRejectsMismatchedResponseAndCNAMECycle(t *testing.T) {
	allowed := dnsName(t, "allowed.example.")
	alias := dnsName(t, "alias.example.")
	for _, test := range []struct {
		name    string
		respond func(dnsmessage.Message) dnsmessage.Message
	}{
		{name: "mismatched id", respond: func(query dnsmessage.Message) dnsmessage.Message {
			return dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID + 1, Response: true}, Questions: query.Questions}
		}},
		{name: "not a response", respond: func(query dnsmessage.Message) dnsmessage.Message {
			return dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID}, Questions: query.Questions}
		}},
		{name: "mismatched question", respond: func(query dnsmessage.Message) dnsmessage.Message {
			query.Questions[0].Name = alias
			return dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions}
		}},
		{name: "CNAME cycle", respond: func(query dnsmessage.Message) dnsmessage.Message {
			return dnsmessage.Message{
				Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions,
				Answers: []dnsmessage.Resource{
					{Header: dnsmessage.ResourceHeader{Name: allowed, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.CNAMEResource{CNAME: alias}},
					{Header: dnsmessage.ResourceHeader{Name: alias, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.CNAMEResource{CNAME: allowed}},
				},
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := startUDPUpstream(t, test.respond)
			engine := NewEngine()
			engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{
				SandboxIp: "10.0.0.2", Policy: strictPolicy("allowed.example"), UpstreamNameservers: []string{upstream},
			}})
			if _, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "allowed.example.", dnsmessage.TypeA), false); err == nil {
				t.Fatal("ambiguous strict DNS response was accepted")
			}
			if engine.authorized("10.0.0.2", "allowed.example", mustAddr("192.0.2.20")) {
				t.Fatal("failed strict DNS response left an authorization")
			}
		})
	}
}

func TestDNSPreservesEDNSAndDNSSECResponseBits(t *testing.T) {
	root := dnsName(t, ".")
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{
			Header:      dnsmessage.Header{ID: query.Header.ID, Response: true, AuthenticData: true},
			Questions:   query.Questions,
			Additionals: query.Additionals,
		}
	})
	engine := NewEngine()
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{
		SandboxIp: "10.0.0.2", Policy: dnsDenyPolicy("blocked.example"), UpstreamNameservers: []string{upstream},
	}})
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 7, RecursionDesired: true, CheckingDisabled: true},
		Questions: []dnsmessage.Question{{Name: dnsName(t, "allowed.example."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
		Additionals: []dnsmessage.Resource{{
			Header: dnsmessage.ResourceHeader{Name: root, Type: dnsmessage.TypeOPT, Class: 1232, TTL: 0x8000},
			Body:   &dnsmessage.OPTResource{Options: []dnsmessage.Option{{Code: 12, Data: []byte{1, 2, 3, 4}}}},
		}},
	}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	responseWire, err := engine.resolveDNS("10.0.0.2", wire, false)
	if err != nil {
		t.Fatal(err)
	}
	var response dnsmessage.Message
	if err := response.Unpack(responseWire); err != nil {
		t.Fatal(err)
	}
	if !response.Header.AuthenticData || len(response.Additionals) != 1 {
		t.Fatalf("DNSSEC/EDNS response metadata was not preserved: %#v", response)
	}
	opt, ok := response.Additionals[0].Body.(*dnsmessage.OPTResource)
	if !ok || response.Additionals[0].Header.TTL != 0x8000 || len(opt.Options) != 1 || string(opt.Options[0].Data) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("EDNS option was not preserved: %#v", response.Additionals)
	}
}

func TestStrictNegativeResponseCreatesNoAuthorization(t *testing.T) {
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{
			Header: dnsmessage.Header{ID: query.Header.ID, Response: true, RCode: dnsmessage.RCodeNameError}, Questions: query.Questions,
		}
	})
	engine := NewEngine()
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{
		SandboxIp: "10.0.0.2", Policy: strictPolicy("missing.example"), UpstreamNameservers: []string{upstream},
	}})
	if _, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "missing.example.", dnsmessage.TypeA), false); err != nil {
		t.Fatal(err)
	}
	if len(engine.auth) != 0 {
		t.Fatalf("negative DNS response created authorization state: %#v", engine.auth)
	}
}

func TestDNSDenyRejectsDeniedCNAMEChain(t *testing.T) {
	alias := dnsName(t, "alias.example.")
	target := dnsName(t, "blocked.example.")
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: alias, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.CNAMEResource{CNAME: target}}}}
	})
	engine := NewEngine()
	policy := dnsDenyPolicy("blocked.example")
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{SandboxIp: "10.0.0.2", Policy: policy, UpstreamNameservers: []string{upstream}}})
	wire, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "alias.example.", dnsmessage.TypeA), false)
	if err != nil {
		t.Fatal(err)
	}
	var response dnsmessage.Message
	if err := response.Unpack(wire); err != nil {
		t.Fatal(err)
	}
	if response.Header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("rcode = %v", response.Header.RCode)
	}
}

func TestDNSDenyRejectsDeniedCNAMEInAdditionalSection(t *testing.T) {
	alias := dnsName(t, "alias.example.")
	target := dnsName(t, "blocked.example.")
	upstream := startUDPUpstream(t, func(query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{
			Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions,
			Additionals: []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{Name: alias, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60},
				Body:   &dnsmessage.CNAMEResource{CNAME: target},
			}},
		}
	})
	engine := NewEngine()
	engine.SetPolicies([]*runtimeegressv1.PreparedEgressPolicy{{
		SandboxIp: "10.0.0.2", Policy: dnsDenyPolicy("blocked.example"), UpstreamNameservers: []string{upstream},
	}})
	wire, err := engine.resolveDNS("10.0.0.2", dnsQuery(t, "alias.example.", dnsmessage.TypeA), false)
	if err != nil {
		t.Fatal(err)
	}
	var response dnsmessage.Message
	if err := response.Unpack(wire); err != nil {
		t.Fatal(err)
	}
	if response.Header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("rcode = %v, want REFUSED", response.Header.RCode)
	}
}

func TestStrictDomainDNSNeverAuthorizesPrivateOrMetadataAddresses(t *testing.T) {
	for _, value := range []string{"10.0.0.1", "169.254.169.254", "fd00::1", "::1"} {
		if eligibleDomainAddress(netip.MustParseAddr(value)) {
			t.Fatalf("domain policy authorized reserved address %s", value)
		}
	}
	if !eligibleDomainAddress(netip.MustParseAddr("93.184.216.34")) {
		t.Fatal("public unicast address was rejected")
	}
}

func dnsDenyPolicy(domains ...string) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}
}

func strictPolicy(domains ...string) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: domains}}}
}

func dnsName(t *testing.T, value string) dnsmessage.Name {
	t.Helper()
	name, err := dnsmessage.NewName(value)
	if err != nil {
		t.Fatal(err)
	}
	return name
}
func dnsQuery(t *testing.T, name string, kind dnsmessage.Type) []byte {
	t.Helper()
	wire, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 7, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsName(t, name), Type: kind, Class: dnsmessage.ClassINET}}}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func startUDPUpstream(t *testing.T, respond func(dnsmessage.Message) dnsmessage.Message) string {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, source, err := listener.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil {
				continue
			}
			response := respond(query)
			wire, err := response.Pack()
			if err == nil {
				_, _ = listener.WriteToUDP(wire, source)
			}
		}
	}()
	return net.JoinHostPort(testNodeIPv4(t).String(), fmt.Sprint(listener.LocalAddr().(*net.UDPAddr).Port))
}

func testNodeIPv4(t *testing.T) net.IP {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range addresses {
		prefix, err := netip.ParsePrefix(value.String())
		if err != nil {
			continue
		}
		address := prefix.Addr().Unmap()
		if address.Is4() && address.IsGlobalUnicast() && !address.IsLoopback() {
			return net.IP(address.AsSlice())
		}
	}
	t.Fatal("test host has no non-loopback IPv4 address")
	return nil
}

func mustAddr(value string) netip.Addr { return netip.MustParseAddr(value) }
