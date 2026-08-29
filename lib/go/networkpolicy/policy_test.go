package networkpolicy

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestNormalizeStrictCanonicalizesDomainsCIDRsAndPorts(t *testing.T) {
	in := strictSpec(
		[]string{"BÜCHER.example.", "*.GitHub.COM", "bücher.example"},
		[]*commonv1.CIDREgressRule{
			{Cidr: "10.0.0.4/24", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 443, End: 443}, {Start: 80, End: 82}}},
			{Cidr: "10.0.0.0/24", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 81, End: 90}}},
		},
	)
	got, err := Normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	want := strictSpec(
		[]string{"*.github.com", "xn--bcher-kva.example"},
		[]*commonv1.CIDREgressRule{{Cidr: "10.0.0.0/24", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 80, End: 90}, {Start: 443, End: 443}}}},
	)
	if !proto.Equal(got, want) {
		t.Fatalf("Normalize() = %v, want %v", got, want)
	}
}

func TestNormalizeIsolatedAsStrictDenyAll(t *testing.T) {
	got, err := Normalize(&commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED})
	if err != nil {
		t.Fatal(err)
	}
	if Mode(got) != EnforcementStrict || StrictNeedsEgressd(got) || got.GetMode() != commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		t.Fatalf("isolated normalization = %#v", got)
	}
}

func TestNormalizeExplicitStrictDenyAllUsesIsolatedDataplane(t *testing.T) {
	got, err := Normalize(strictSpec(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.GetMode() != commonv1.NetworkMode_NETWORK_MODE_ISOLATED || !IsStrictDenyAll(got) || StrictNeedsEgressd(got) {
		t.Fatalf("strict deny-all normalization = %#v", got)
	}
}

func TestNormalizeRejectsPolicyConflictsAndMalformedRules(t *testing.T) {
	tooMany := make([]string, MaxRules+1)
	for index := range tooMany {
		tooMany[index] = strings.Repeat("a", 1) + string(rune('a'+index%26)) + ".example" + string(rune(0x100+index))
	}
	tests := []struct {
		name string
		spec *commonv1.NetworkSpec
	}{
		{name: "host policy", spec: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST, EgressPolicy: strictPolicy(nil, nil)}},
		{name: "isolated allow", spec: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED, EgressPolicy: strictPolicy([]string{"example.com"}, nil)}},
		{name: "empty policy wrapper", spec: &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{}}},
		{name: "empty dns deny", spec: dnsDenySpec(nil)},
		{name: "URL domain", spec: dnsDenySpec([]string{"https://example.com"})},
		{name: "IP domain", spec: dnsDenySpec([]string{"192.0.2.1"})},
		{name: "bad wildcard", spec: dnsDenySpec([]string{"foo.*.example.com"})},
		{name: "too long domain", spec: dnsDenySpec([]string{strings.Repeat("a", 64) + ".example"})},
		{name: "reserved CIDR", spec: strictSpec(nil, []*commonv1.CIDREgressRule{{Cidr: "169.254.169.254/32", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 80, End: 80}}}})},
		{name: "metadata CIDR", spec: strictSpec(nil, []*commonv1.CIDREgressRule{{Cidr: "100.100.100.200/32", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 80, End: 80}}}})},
		{name: "unspecified protocol", spec: strictSpec(nil, []*commonv1.CIDREgressRule{{Cidr: "10.0.0.0/8", Ports: []*commonv1.PortRange{{Start: 80, End: 80}}}})},
		{name: "missing ports", spec: strictSpec(nil, []*commonv1.CIDREgressRule{{Cidr: "10.0.0.0/8", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP}})},
		{name: "bad port range", spec: strictSpec(nil, []*commonv1.CIDREgressRule{{Cidr: "10.0.0.0/8", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 443, End: 80}}}})},
		{name: "too many", spec: dnsDenySpec(tooMany)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Normalize(tt.spec); err == nil {
				t.Fatalf("Normalize(%v) succeeded", tt.spec)
			}
		})
	}
}

func TestNormalizeDNSDenyDeduplicatesAndClassifies(t *testing.T) {
	got, err := Normalize(dnsDenySpec([]string{"Example.COM.", "example.com", "*.example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if Mode(got) != EnforcementDNSDeny {
		t.Fatalf("Mode() = %d", Mode(got))
	}
	want := []string{"*.example.com", "example.com"}
	if !proto.Equal(got.GetEgressPolicy().GetDnsDeny(), &commonv1.DnsDenyPolicy{DeniedDomains: want}) {
		t.Fatalf("domains = %v, want %v", got.GetEgressPolicy().GetDnsDeny().GetDeniedDomains(), want)
	}
}

func strictSpec(domains []string, cidrs []*commonv1.CIDREgressRule) *commonv1.NetworkSpec {
	return &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, EgressPolicy: strictPolicy(domains, cidrs)}
}

func strictPolicy(domains []string, cidrs []*commonv1.CIDREgressRule) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: domains, AllowedCidrs: cidrs}}}
}

func dnsDenySpec(domains []string) *commonv1.NetworkSpec {
	return &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}}
}
