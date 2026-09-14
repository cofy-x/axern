package verifyutil

import "testing"

func TestValidateTCFilterOutput(t *testing.T) {
	if err := validateTCFilterOutput("filter protocol all pref 1 bpf chain 0", "eth0", "ingress"); err != nil {
		t.Fatalf("validateTCFilterOutput returned error: %v", err)
	}
	if err := validateTCFilterOutput("filter protocol all pref 1 flower", "eth0", "egress"); err == nil {
		t.Fatal("validateTCFilterOutput should reject output without bpf marker")
	}
}
