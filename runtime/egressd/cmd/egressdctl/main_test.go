package main

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestParseCIDRRules(t *testing.T) {
	rules, err := parseCIDRRules([]string{"tcp@192.0.2.0/24@22", "udp@2001:db8::/32@5353-5355"})
	if err != nil {
		t.Fatalf("parseCIDRRules() error = %v", err)
	}
	if len(rules) != 2 || rules[0].GetProtocol() != commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP || rules[0].GetPorts()[0].GetStart() != 22 {
		t.Fatalf("unexpected TCP rule: %+v", rules)
	}
	if rules[1].GetProtocol() != commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP || rules[1].GetPorts()[0].GetStart() != 5353 || rules[1].GetPorts()[0].GetEnd() != 5355 {
		t.Fatalf("unexpected UDP rule: %+v", rules[1])
	}
}

func TestParseCIDRRulesRejectsInvalidPorts(t *testing.T) {
	for _, value := range []string{"tcp@192.0.2.0/24@0", "tcp@192.0.2.0/24@23-22", "icmp@192.0.2.0/24@8"} {
		if _, err := parseCIDRRules([]string{value}); err == nil {
			t.Fatalf("parseCIDRRules(%q) unexpectedly succeeded", value)
		}
	}
}
