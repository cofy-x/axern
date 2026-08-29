package enforcement

import (
	"net"
	"net/netip"
	"testing"

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
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
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
	return listener.LocalAddr().String()
}

func mustAddr(value string) netip.Addr { return netip.MustParseAddr(value) }
