package axernsdk

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"golang.org/x/net/idna"
	"google.golang.org/protobuf/proto"
)

const maxNetworkPolicyRules = 256

var forbiddenNetworkPolicyCIDRs = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/32"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.192/32"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type EgressProtocol string

const (
	EgressProtocolTCP EgressProtocol = "tcp"
	EgressProtocolUDP EgressProtocol = "udp"
)

type PortRange struct {
	Start uint32
	End   uint32
}

type CIDRRule struct {
	CIDR     string
	Protocol EgressProtocol
	Ports    []PortRange
}

type NetworkPolicy struct {
	wire *commonv1.NetworkEgressPolicy
}

func NewStrictNetworkPolicy(domains []string, cidrRules []CIDRRule) (*NetworkPolicy, error) {
	normalizedDomains, err := normalizePolicyDomains(domains)
	if err != nil {
		return nil, err
	}
	normalizedCIDRs, err := normalizePolicyCIDRs(cidrRules)
	if err != nil {
		return nil, err
	}
	if len(normalizedDomains)+len(normalizedCIDRs) > maxNetworkPolicyRules {
		return nil, fmt.Errorf("network policy may contain at most %d rules", maxNetworkPolicyRules)
	}
	return &NetworkPolicy{wire: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: normalizedDomains, AllowedCidrs: normalizedCIDRs}}}}, nil
}

func AllowDomainNetworkPolicy(domains ...string) (*NetworkPolicy, error) {
	return NewStrictNetworkPolicy(domains, nil)
}

func DenyDNSNetworkPolicy(domains ...string) (*NetworkPolicy, error) {
	normalized, err := normalizePolicyDomains(domains)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("deny DNS policy requires at least one domain")
	}
	return &NetworkPolicy{wire: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: normalized}}}}, nil
}

func DenyAllNetworkPolicy() *NetworkPolicy {
	policy, _ := NewStrictNetworkPolicy(nil, nil)
	return policy
}

func (p *NetworkPolicy) proto() *commonv1.NetworkEgressPolicy {
	if p == nil || p.wire == nil {
		return nil
	}
	return proto.Clone(p.wire).(*commonv1.NetworkEgressPolicy)
}

func normalizePolicyDomains(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		raw := strings.ToLower(strings.TrimSpace(value))
		wildcard := strings.HasPrefix(raw, "*.")
		if wildcard {
			raw = strings.TrimPrefix(raw, "*.")
		}
		raw = strings.TrimSuffix(raw, ".")
		if raw == "" || strings.ContainsAny(raw, "/:@?#*") {
			return nil, fmt.Errorf("invalid domain rule %q", value)
		}
		if _, err := netip.ParseAddr(raw); err == nil {
			return nil, fmt.Errorf("domain rule must not be an IP literal: %q", value)
		}
		ascii, err := idna.Lookup.ToASCII(raw)
		if err != nil || len(ascii) > 253 {
			return nil, fmt.Errorf("invalid domain rule %q", value)
		}
		for _, label := range strings.Split(ascii, ".") {
			if label == "" || len(label) > 63 {
				return nil, fmt.Errorf("invalid domain rule %q", value)
			}
		}
		if wildcard {
			ascii = "*." + ascii
		}
		if _, ok := seen[ascii]; !ok {
			seen[ascii] = struct{}{}
			result = append(result, ascii)
		}
	}
	return result, nil
}

func normalizePolicyCIDRs(values []CIDRRule) ([]*commonv1.CIDREgressRule, error) {
	seen := map[string]*commonv1.CIDREgressRule{}
	for index, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value.CIDR))
		if err != nil {
			return nil, fmt.Errorf("CIDR rule %d: %w", index, err)
		}
		prefix = prefix.Masked()
		for _, forbidden := range forbiddenNetworkPolicyCIDRs {
			if prefix.Addr().BitLen() == forbidden.Addr().BitLen() && prefix.Bits() >= forbidden.Bits() && forbidden.Contains(prefix.Addr()) {
				return nil, fmt.Errorf("CIDR rule %d targets a protected address range", index)
			}
		}
		protocol := commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP
		if value.Protocol == EgressProtocolUDP {
			protocol = commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP
		} else if value.Protocol != EgressProtocolTCP {
			return nil, fmt.Errorf("CIDR rule %d protocol must be tcp or udp", index)
		}
		if len(value.Ports) == 0 {
			return nil, fmt.Errorf("CIDR rule %d requires at least one port range", index)
		}
		ports := make([]*commonv1.PortRange, 0, len(value.Ports))
		for _, value := range value.Ports {
			end := value.End
			if end == 0 {
				end = value.Start
			}
			if value.Start == 0 || end < value.Start || end > 65535 {
				return nil, fmt.Errorf("CIDR rule %d ports must be within 1..65535", index)
			}
			ports = append(ports, &commonv1.PortRange{Start: value.Start, End: end})
		}
		sort.Slice(ports, func(i, j int) bool {
			return ports[i].GetStart() < ports[j].GetStart() || ports[i].GetStart() == ports[j].GetStart() && ports[i].GetEnd() < ports[j].GetEnd()
		})
		var keyBuilder strings.Builder
		fmt.Fprintf(&keyBuilder, "%s|%d", prefix, protocol)
		for _, port := range ports {
			fmt.Fprintf(&keyBuilder, "|%d-%d", port.GetStart(), port.GetEnd())
		}
		key := keyBuilder.String()
		seen[key] = &commonv1.CIDREgressRule{Cidr: prefix.String(), Protocol: protocol, Ports: ports}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*commonv1.CIDREgressRule, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result, nil
}
