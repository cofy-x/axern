package verifyutil

import "testing"

func TestValidateIptablesRuleOutput(t *testing.T) {
	output := "-A PREROUTING -p tcp --dport 18080 -j DNAT\n"
	if err := validateIptablesRuleOutput(output, "nat", "PREROUTING", "--dport 18080 -j DNAT", true); err != nil {
		t.Fatalf("validateIptablesRuleOutput returned error: %v", err)
	}
	if err := validateIptablesRuleOutput(output, "nat", "PREROUTING", "--dport 18080 -j DNAT", false); err == nil {
		t.Fatal("validateIptablesRuleOutput should reject unexpected rule presence")
	}
}

func TestValidateTCFilterOutput(t *testing.T) {
	if err := validateTCFilterOutput("filter protocol all pref 1 bpf chain 0", "eth0", "ingress"); err != nil {
		t.Fatalf("validateTCFilterOutput returned error: %v", err)
	}
	if err := validateTCFilterOutput("filter protocol all pref 1 flower", "eth0", "egress"); err == nil {
		t.Fatal("validateTCFilterOutput should reject output without bpf marker")
	}
}
