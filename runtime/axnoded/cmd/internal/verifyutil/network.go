package verifyutil

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
)

func LoadBPFNetStatus(pinPath string) (bpfnet.Status, error) {
	status, err := bpfnetstatus.Load(pinPath)
	if err != nil {
		return bpfnet.Status{}, err
	}
	if len(status.Attachment.UplinkDevices) == 0 {
		return bpfnet.Status{}, fmt.Errorf("bpfnet attachment readiness did not record any uplink devices")
	}
	return status, nil
}

func FindBPFNetService(status bpfnet.Status, protocol string, listenPort uint16) (bpfnet.Service, error) {
	return bpfnetstatus.FindService(status, protocol, listenPort)
}

func AssertIptablesRule(table, chain, needle string) error {
	output, err := exec.Command("iptables", "-t", table, "-S", chain).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -t %s -S %s failed: %w: %s", table, chain, err, strings.TrimSpace(string(output)))
	}
	return validateIptablesRuleOutput(string(output), table, chain, needle, true)
}

func AssertIptablesRuleAbsent(table, chain, needle string) error {
	output, err := exec.Command("iptables", "-t", table, "-S", chain).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -t %s -S %s failed: %w: %s", table, chain, err, strings.TrimSpace(string(output)))
	}
	return validateIptablesRuleOutput(string(output), table, chain, needle, false)
}

func AssertTCFiltersAttached(uplinks []string) error {
	for _, uplink := range uplinks {
		if err := AssertTCFilter(uplink, "ingress"); err != nil {
			return err
		}
		if err := AssertTCFilter(uplink, "egress"); err != nil {
			return err
		}
	}
	return nil
}

func AssertTCFilter(device, direction string) error {
	output, err := exec.Command("tc", "filter", "show", "dev", device, direction).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tc filter show dev %s %s failed: %w: %s", device, direction, err, strings.TrimSpace(string(output)))
	}
	return validateTCFilterOutput(string(output), device, direction)
}

func validateIptablesRuleOutput(output, table, chain, needle string, shouldContain bool) error {
	contains := strings.Contains(output, needle)
	if shouldContain && !contains {
		return fmt.Errorf("iptables %s/%s missing rule containing %q: %s", table, chain, needle, strings.TrimSpace(output))
	}
	if !shouldContain && contains {
		return fmt.Errorf("iptables %s/%s unexpectedly contained %q: %s", table, chain, needle, strings.TrimSpace(output))
	}
	return nil
}

func validateTCFilterOutput(output, device, direction string) error {
	if !strings.Contains(output, "bpf") {
		return fmt.Errorf("tc filter show dev %s %s missing bpf attachment: %s", device, direction, strings.TrimSpace(output))
	}
	return nil
}
