package enforcement

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

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
	mu       sync.Mutex
	engine   *Engine
	revision int64
	healthy  bool
	reason   string
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
	e.engine.SetPolicies(records)
	e.revision++
	e.healthy = true
	e.reason = ""
	return nil
}

func (e *NFTExecutor) Health(ctx context.Context) policy.EnforcementHealth {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.healthy {
		return policy.EnforcementHealth{Revision: e.revision, Reason: e.reason}
	}
	if err := runNFT(ctx, "list", "table", "inet", nftTable); err != nil {
		e.healthy = false
		e.reason = fmt.Sprintf("nft ruleset health: %v", err)
		return policy.EnforcementHealth{Revision: e.revision, Reason: e.reason}
	}
	return policy.EnforcementHealth{DNSPolicyReady: true, StrictEgressReady: true, Revision: e.revision}
}

func runNFT(ctx context.Context, args ...any) error {
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
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func RenderNFT(records []*runtimeegressv1.PreparedEgressPolicy) ([]byte, error) {
	records = append([]*runtimeegressv1.PreparedEgressPolicy(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].GetSandboxIp() < records[j].GetSandboxIp() })
	var out strings.Builder
	out.WriteString("destroy table inet " + nftTable + "\n")
	out.WriteString("table inet " + nftTable + " {\n")
	out.WriteString(" chain ingress_proxy { type filter hook prerouting priority mangle; policy accept;\n")
	fmt.Fprintf(&out, "  meta mark 0x%x return\n", bypassMark)
	for _, record := range records {
		family, source, err := nftSource(record.GetSandboxIp())
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "  %s saddr %s udp dport 53 meta mark set 0x%x tproxy %s to :%d accept\n", family, source, policyMark, family, dnsProxyPort)
		fmt.Fprintf(&out, "  %s saddr %s tcp dport 53 meta mark set 0x%x tproxy %s to :%d accept\n", family, source, policyMark, family, dnsProxyPort)
		if strict := record.GetPolicy().GetStrict(); strict != nil {
			for _, rule := range strict.GetAllowedCidrs() {
				writeCIDRRule(&out, family, source, rule, "return")
			}
			fmt.Fprintf(&out, "  %s saddr %s tcp dport 80 meta mark set 0x%x tproxy %s to :%d accept\n", family, source, policyMark, family, httpProxyPort)
			fmt.Fprintf(&out, "  %s saddr %s tcp dport 443 meta mark set 0x%x tproxy %s to :%d accept\n", family, source, policyMark, family, httpsProxyPort)
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
		fmt.Fprintf(&out, "  %s saddr %s drop\n", family, source)
	}
	out.WriteString(" }\n}\n")
	return []byte(out.String()), nil
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
	fmt.Fprintf(out, "  %s saddr %s %s daddr %s %s dport %s %s\n", sourceFamily, source, destinationFamily, prefix.String(), protocol, portExpr, verdict)
}
