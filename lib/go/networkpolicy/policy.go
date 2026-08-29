// Package networkpolicy owns the canonical sandbox network-policy validation
// and normalization rules shared by the control plane and node runtime.
package networkpolicy

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"golang.org/x/net/idna"
)

const MaxRules = 256

type EnforcementMode uint8

const (
	EnforcementNone EnforcementMode = iota
	EnforcementDNSDeny
	EnforcementStrict
)

var forbiddenCIDRs = []netip.Prefix{
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

// Normalize validates and returns a deterministic policy representation.
// NETWORK_MODE_ISOLATED is represented as strict deny-all while retaining the
// legacy mode so nodes can enforce it without egressd during a rolling upgrade.
func Normalize(in *commonv1.NetworkSpec) (*commonv1.NetworkSpec, error) {
	if in == nil {
		return nil, nil
	}
	mode := in.GetMode()
	switch mode {
	case commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED,
		commonv1.NetworkMode_NETWORK_MODE_DEFAULT,
		commonv1.NetworkMode_NETWORK_MODE_ISOLATED,
		commonv1.NetworkMode_NETWORK_MODE_HOST:
	default:
		return nil, fmt.Errorf("unknown network mode %d", mode)
	}
	if mode == commonv1.NetworkMode_NETWORK_MODE_HOST && in.GetEgressPolicy() != nil {
		return nil, fmt.Errorf("host network mode conflicts with an egress policy")
	}
	if mode == commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		if policy := in.GetEgressPolicy(); policy != nil {
			strict := policy.GetStrict()
			if strict == nil || len(strict.GetAllowedDomains()) != 0 || len(strict.GetAllowedCidrs()) != 0 {
				return nil, fmt.Errorf("isolated network mode permits only strict deny-all")
			}
		}
		return &commonv1.NetworkSpec{
			Mode: mode,
			EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
				Strict: &commonv1.StrictEgressPolicy{},
			}},
		}, nil
	}

	out := &commonv1.NetworkSpec{Mode: mode}
	policy := in.GetEgressPolicy()
	if policy == nil {
		return out, nil
	}
	switch typed := policy.GetPolicy().(type) {
	case *commonv1.NetworkEgressPolicy_Strict:
		strict, err := normalizeStrict(typed.Strict)
		if err != nil {
			return nil, fmt.Errorf("strict policy: %w", err)
		}
		if len(strict.GetAllowedDomains()) == 0 && len(strict.GetAllowedCidrs()) == 0 {
			out.Mode = commonv1.NetworkMode_NETWORK_MODE_ISOLATED
		}
		out.EgressPolicy = &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: strict}}
	case *commonv1.NetworkEgressPolicy_DnsDeny:
		dnsDeny, err := normalizeDNSDeny(typed.DnsDeny)
		if err != nil {
			return nil, fmt.Errorf("dns deny policy: %w", err)
		}
		out.EgressPolicy = &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: dnsDeny}}
	default:
		return nil, fmt.Errorf("egress policy kind is required")
	}
	return out, nil
}

func Validate(in *commonv1.NetworkSpec) error {
	_, err := Normalize(in)
	return err
}

func Mode(in *commonv1.NetworkSpec) EnforcementMode {
	if in == nil {
		return EnforcementNone
	}
	if in.GetMode() == commonv1.NetworkMode_NETWORK_MODE_ISOLATED || in.GetEgressPolicy().GetStrict() != nil {
		return EnforcementStrict
	}
	if in.GetEgressPolicy().GetDnsDeny() != nil {
		return EnforcementDNSDeny
	}
	return EnforcementNone
}

func StrictNeedsEgressd(in *commonv1.NetworkSpec) bool {
	strict := in.GetEgressPolicy().GetStrict()
	return strict != nil && (len(strict.GetAllowedDomains()) != 0 || len(strict.GetAllowedCidrs()) != 0)
}

func IsStrictDenyAll(in *commonv1.NetworkSpec) bool {
	strict := in.GetEgressPolicy().GetStrict()
	return strict != nil && len(strict.GetAllowedDomains()) == 0 && len(strict.GetAllowedCidrs()) == 0
}

func normalizeStrict(in *commonv1.StrictEgressPolicy) (*commonv1.StrictEgressPolicy, error) {
	if in == nil {
		return nil, fmt.Errorf("policy body is required")
	}
	domains, err := normalizeDomains(in.GetAllowedDomains())
	if err != nil {
		return nil, fmt.Errorf("allowed_domains: %w", err)
	}
	cidrs, err := normalizeCIDRs(in.GetAllowedCidrs())
	if err != nil {
		return nil, fmt.Errorf("allowed_cidrs: %w", err)
	}
	if len(domains)+len(cidrs) > MaxRules {
		return nil, fmt.Errorf("normalized rule count exceeds %d", MaxRules)
	}
	return &commonv1.StrictEgressPolicy{AllowedDomains: domains, AllowedCidrs: cidrs}, nil
}

func normalizeDNSDeny(in *commonv1.DnsDenyPolicy) (*commonv1.DnsDenyPolicy, error) {
	if in == nil {
		return nil, fmt.Errorf("policy body is required")
	}
	domains, err := normalizeDomains(in.GetDeniedDomains())
	if err != nil {
		return nil, fmt.Errorf("denied_domains: %w", err)
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("at least one denied domain is required")
	}
	if len(domains) > MaxRules {
		return nil, fmt.Errorf("normalized rule count exceeds %d", MaxRules)
	}
	return &commonv1.DnsDenyPolicy{DeniedDomains: domains}, nil
}

func normalizeDomains(in []string) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	for index, raw := range in {
		domain, err := normalizeDomain(raw)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", index, err)
		}
		seen[domain] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for domain := range seen {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeDomain(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, ".")
	wildcard := strings.HasPrefix(value, "*.")
	if wildcard {
		value = strings.TrimPrefix(value, "*.")
	}
	if value == "" || strings.Contains(value, "*") || strings.ContainsAny(value, "/:@?#[]%") {
		return "", fmt.Errorf("%q is not a DNS name or leading wildcard", raw)
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil {
		return "", fmt.Errorf("%q is not a valid IDNA name: %w", raw, err)
	}
	ascii = strings.ToLower(ascii)
	if len(ascii) > 253 {
		return "", fmt.Errorf("%q exceeds 253 bytes after IDNA normalization", raw)
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 {
			return "", fmt.Errorf("%q contains an empty label or a label exceeding 63 bytes", raw)
		}
	}
	if address, err := netip.ParseAddr(ascii); err == nil && address.IsValid() {
		return "", fmt.Errorf("%q is an IP literal, not a domain", raw)
	}
	if wildcard {
		return "*." + ascii, nil
	}
	return ascii, nil
}

type cidrKey struct {
	cidr     string
	protocol commonv1.EgressProtocol
}

func normalizeCIDRs(in []*commonv1.CIDREgressRule) ([]*commonv1.CIDREgressRule, error) {
	grouped := make(map[cidrKey][]*commonv1.PortRange, len(in))
	for index, rule := range in {
		if rule == nil {
			return nil, fmt.Errorf("rule %d must not be nil", index)
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rule.GetCidr()))
		if err != nil {
			return nil, fmt.Errorf("rule %d has invalid CIDR: %w", index, err)
		}
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("rule %d uses an IPv4-mapped IPv6 CIDR", index)
		}
		prefix = prefix.Masked()
		if forbiddenPrefix(prefix) {
			return nil, fmt.Errorf("rule %d targets a reserved loopback, link-local, multicast, unspecified, or metadata range", index)
		}
		protocol := rule.GetProtocol()
		if protocol != commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP && protocol != commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP {
			return nil, fmt.Errorf("rule %d protocol must be TCP or UDP", index)
		}
		if len(rule.GetPorts()) == 0 {
			return nil, fmt.Errorf("rule %d requires at least one port range", index)
		}
		key := cidrKey{cidr: prefix.String(), protocol: protocol}
		grouped[key] = append(grouped[key], rule.GetPorts()...)
	}
	keys := make([]cidrKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].cidr == keys[j].cidr {
			return keys[i].protocol < keys[j].protocol
		}
		return keys[i].cidr < keys[j].cidr
	})
	out := make([]*commonv1.CIDREgressRule, 0, len(keys))
	for _, key := range keys {
		ports, err := normalizePorts(grouped[key])
		if err != nil {
			return nil, fmt.Errorf("CIDR %s: %w", key.cidr, err)
		}
		out = append(out, &commonv1.CIDREgressRule{Cidr: key.cidr, Protocol: key.protocol, Ports: ports})
	}
	return out, nil
}

func normalizePorts(in []*commonv1.PortRange) ([]*commonv1.PortRange, error) {
	out := make([]*commonv1.PortRange, 0, len(in))
	for index, ports := range in {
		if ports == nil || ports.GetStart() == 0 || ports.GetStart() > 65535 || ports.GetEnd() == 0 || ports.GetEnd() > 65535 || ports.GetStart() > ports.GetEnd() {
			return nil, fmt.Errorf("port range %d must satisfy 1 <= start <= end <= 65535", index)
		}
		out = append(out, &commonv1.PortRange{Start: ports.GetStart(), End: ports.GetEnd()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetStart() == out[j].GetStart() {
			return out[i].GetEnd() < out[j].GetEnd()
		}
		return out[i].GetStart() < out[j].GetStart()
	})
	merged := out[:0]
	for _, current := range out {
		if len(merged) == 0 || current.GetStart() > merged[len(merged)-1].GetEnd()+1 {
			merged = append(merged, current)
			continue
		}
		if current.GetEnd() > merged[len(merged)-1].GetEnd() {
			merged[len(merged)-1].End = current.GetEnd()
		}
	}
	return merged, nil
}

func forbiddenPrefix(candidate netip.Prefix) bool {
	for _, forbidden := range forbiddenCIDRs {
		if candidate.Addr().BitLen() == forbidden.Addr().BitLen() && candidate.Bits() >= forbidden.Bits() && forbidden.Contains(candidate.Addr()) {
			return true
		}
	}
	return false
}
