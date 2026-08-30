package enforcement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	"github.com/cofy-x/axern/runtime/egressd/internal/policy"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

const nftTable = "axern_egress"

const (
	policyMark = 0xA6E1
	bypassMark = 0xA6E2
)

type NFTExecutor struct {
	mu            sync.Mutex
	engine        *Engine
	revision      int64
	healthy       bool
	reason        string
	expectedProof []string
}

func NewNFTExecutor(ctx context.Context) (*NFTExecutor, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return nil, fmt.Errorf("nft is required: %w", err)
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return nil, fmt.Errorf("ip is required: %w", err)
	}
	if err := configurePolicyRouting(ctx); err != nil {
		return nil, err
	}
	engine := NewEngine()
	if err := engine.Start(ctx); err != nil {
		return nil, err
	}
	return &NFTExecutor{engine: engine}, nil
}

func (e *NFTExecutor) Close() error { return e.engine.Close() }

func (e *NFTExecutor) Reconcile(ctx context.Context, records []*runtimeegressv1.PreparedEgressPolicy) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, record := range records {
		if !networkpolicy.RequiresDNSUpstreams(record.GetPolicy()) {
			continue
		}
		if _, err := dnsforward.ParseUpstreams(record.GetUpstreamNameservers()); err != nil {
			e.healthy = false
			e.reason = err.Error()
			return fmt.Errorf("policy %s upstreams: %w", record.GetAllocationID(), err)
		}
	}
	script, err := RenderNFT(records)
	if err != nil {
		e.healthy = false
		e.reason = err.Error()
		return err
	}
	if err := ensureNFTTable(ctx); err != nil {
		e.healthy = false
		e.reason = err.Error()
		return err
	}
	if err := runNFT(ctx, "-c", "-f", "-", script); err != nil {
		e.healthy = false
		e.reason = err.Error()
		return fmt.Errorf("validate nft policy: %w", err)
	}
	if err := runNFT(ctx, "-f", "-", script); err != nil {
		e.healthy = false
		e.reason = err.Error()
		return fmt.Errorf("apply nft policy: %w", err)
	}
	e.expectedProof = nftManagedProof(script)
	e.engine.SetPolicies(records)
	e.revision++
	e.healthy = true
	e.reason = ""
	return nil
}

func ensureNFTTable(ctx context.Context) error {
	if _, err := runNFTOutput(ctx, "list", "table", "inet", nftTable); err == nil {
		return nil
	}
	if err := runNFT(ctx, "add", "table", "inet", nftTable); err != nil {
		return fmt.Errorf("create nft policy table: %w", err)
	}
	return nil
}

func (e *NFTExecutor) Health(ctx context.Context) policy.EnforcementHealth {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.healthy {
		return policy.EnforcementHealth{Revision: e.revision, Reason: e.reason}
	}
	output, err := runNFTOutput(ctx, "list", "table", "inet", nftTable)
	if err != nil {
		e.healthy = false
		e.reason = fmt.Sprintf("nft ruleset health: %v", err)
		return policy.EnforcementHealth{Revision: e.revision, Reason: e.reason}
	}
	actualProof := nftManagedProof(output)
	if !equalStrings(e.expectedProof, actualProof) {
		e.healthy = false
		e.reason = fmt.Sprintf("nft ruleset proof mismatch: expected %d managed rules, found %d", len(e.expectedProof), len(actualProof))
		return policy.EnforcementHealth{Revision: e.revision, Reason: e.reason}
	}
	return policy.EnforcementHealth{DNSPolicyReady: true, StrictEgressReady: true, Revision: e.revision}
}

func runNFT(ctx context.Context, args ...any) error {
	_, err := runNFTOutput(ctx, args...)
	return err
}

func runNFTOutput(ctx context.Context, args ...any) ([]byte, error) {
	var input []byte
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if wire, ok := arg.([]byte); ok {
			input = wire
		} else {
			parts = append(parts, fmt.Sprint(arg))
		}
	}
	command := exec.CommandContext(ctx, "nft", parts...)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

var nftManagedComment = regexp.MustCompile(`comment "(axern:[0-9a-f]{16})"`)

func nftManagedProof(wire []byte) []string {
	matches := nftManagedComment.FindAllSubmatch(wire, -1)
	proof := make([]string, 0, len(matches))
	for _, match := range matches {
		proof = append(proof, string(match[1]))
	}
	sort.Strings(proof)
	return proof
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeManagedRule(out *strings.Builder, format string, args ...any) {
	rule := fmt.Sprintf(format, args...)
	digest := sha256.Sum256([]byte(rule))
	fmt.Fprintf(out, "  %s comment \"axern:%x\"\n", rule, digest[:8])
}

func RenderNFT(records []*runtimeegressv1.PreparedEgressPolicy) ([]byte, error) {
	records = append([]*runtimeegressv1.PreparedEgressPolicy(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].GetSandboxIp() < records[j].GetSandboxIp() })
	var out strings.Builder
	out.WriteString("delete table inet " + nftTable + "\n")
	out.WriteString("table inet " + nftTable + " {\n")
	out.WriteString(" chain ingress_proxy { type filter hook prerouting priority mangle; policy accept;\n")
	writeManagedRule(&out, "meta mark 0x%x return", bypassMark)
	for _, record := range records {
		family, source, err := nftSource(record.GetSandboxIp())
		if err != nil {
			return nil, err
		}
		if networkpolicy.RequiresDNSUpstreams(record.GetPolicy()) {
			writeTProxy(&out, family, source, "udp", 53, dnsProxyPort)
			writeTProxy(&out, family, source, "tcp", 53, dnsProxyPort)
		}
		if strict := record.GetPolicy().GetStrict(); strict != nil {
			for _, rule := range strict.GetAllowedCidrs() {
				writeCIDRRule(&out, family, source, rule, "return")
			}
			writeTProxy(&out, family, source, "tcp", 80, httpProxyPort)
			writeTProxy(&out, family, source, "tcp", 443, httpsProxyPort)
		}
	}
	out.WriteString(" }\n chain forward { type filter hook forward priority filter; policy accept;\n")
	for _, record := range records {
		strict := record.GetPolicy().GetStrict()
		if strict == nil {
			continue
		}
		family, source, _ := nftSource(record.GetSandboxIp())
		for _, rule := range strict.GetAllowedCidrs() {
			writeCIDRRule(&out, family, source, rule, "accept")
		}
		writeManagedRule(&out, "%s saddr %s drop", family, source)
	}
	out.WriteString(" }\n chain input { type filter hook input priority filter; policy accept;\n")
	writeManagedRule(&out, "meta mark 0x%x counter accept", policyMark)
	for _, record := range records {
		strict := record.GetPolicy().GetStrict()
		if strict == nil {
			continue
		}
		family, source, _ := nftSource(record.GetSandboxIp())
		for _, rule := range strict.GetAllowedCidrs() {
			writeCIDRRule(&out, family, source, rule, "accept")
		}
		if family == "ip6" {
			// NDP is required for the sandbox veth to reach its gateway. Limit it
			// to the two neighbor-discovery message types and the RFC-mandated
			// hop limit so this does not become general ICMPv6 egress.
			writeManagedRule(&out, "ip6 saddr %s ip6 hoplimit 255 icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert } accept", source)
		}
		writeManagedRule(&out, "%s saddr %s counter drop", family, source)
	}
	out.WriteString(" }\n}\n")
	return []byte(out.String()), nil
}

func writeTProxy(out *strings.Builder, family, source, protocol string, destinationPort, proxyPort int) {
	// TPROXY is non-terminal. If no matching transparent socket is alive, nft
	// continues to the following rule, which makes daemon loss fail closed in
	// prerouting instead of allowing a host wildcard service to receive traffic.
	writeManagedRule(out, "%s saddr %s %s dport %d counter tproxy %s to :%d meta mark set 0x%x accept", family, source, protocol, destinationPort, family, proxyPort, policyMark)
	writeManagedRule(out, "%s saddr %s %s dport %d drop", family, source, protocol, destinationPort)
}

func configurePolicyRouting(ctx context.Context) error {
	commands := [][]string{
		{"-4", "rule", "add", "fwmark", fmt.Sprintf("0x%x", policyMark), "lookup", "166", "priority", "16600"},
		{"-4", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", "166"},
		{"-6", "rule", "add", "fwmark", fmt.Sprintf("0x%x", policyMark), "lookup", "166", "priority", "16600"},
		{"-6", "route", "replace", "local", "::/0", "dev", "lo", "table", "166"},
	}
	for _, args := range commands {
		command := exec.CommandContext(ctx, "ip", args...)
		output, err := command.CombinedOutput()
		if err != nil && !strings.Contains(string(output), "File exists") {
			return fmt.Errorf("configure policy routing %q: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func nftSource(value string) (string, string, error) {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", "", fmt.Errorf("invalid sandbox IP %q", value)
	}
	if addr.Is4() {
		return "ip", addr.String(), nil
	}
	return "ip6", addr.String(), nil
}

func writeCIDRRule(out *strings.Builder, sourceFamily, source string, rule *commonv1.CIDREgressRule, verdict string) {
	if rule == nil {
		return
	}
	prefix, err := netip.ParsePrefix(rule.GetCidr())
	if err != nil {
		return
	}
	destinationFamily := "ip6"
	if prefix.Addr().Is4() {
		destinationFamily = "ip"
	}
	if destinationFamily != sourceFamily {
		return
	}
	protocol := "tcp"
	if rule.GetProtocol() == commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP {
		protocol = "udp"
	}
	ports := make([]string, 0, len(rule.GetPorts()))
	for _, port := range rule.GetPorts() {
		if port.GetStart() == port.GetEnd() {
			ports = append(ports, strconv.FormatUint(uint64(port.GetStart()), 10))
		} else {
			ports = append(ports, fmt.Sprintf("%d-%d", port.GetStart(), port.GetEnd()))
		}
	}
	portExpr := ports[0]
	if len(ports) > 1 {
		portExpr = "{ " + strings.Join(ports, ", ") + " }"
	}
	writeManagedRule(out, "%s saddr %s %s daddr %s %s dport %s %s", sourceFamily, source, destinationFamily, prefix.String(), protocol, portExpr, verdict)
}
