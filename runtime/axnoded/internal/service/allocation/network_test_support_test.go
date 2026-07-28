package allocation

import (
	"fmt"
	"net"
)

type fakeNetworkManager struct {
	failNext bool
	removed  []dnatCall
}

type dnatCall struct {
	Protocol   string
	DstPort    uint16
	TargetIP   string
	TargetPort uint16
}

func (f *fakeNetworkManager) SetupSNATRules(string) error { return nil }

func (f *fakeNetworkManager) CleanupSNATRules(string) error { return nil }

func (f *fakeNetworkManager) SetupNetworkRulesForActivating(net.IP, string) error { return nil }

func (f *fakeNetworkManager) CleanupNetworkRulesForActivating(net.IP) error { return nil }

func (f *fakeNetworkManager) SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	if f.failNext {
		f.failNext = false
		return fmt.Errorf("dnat failed")
	}
	return nil
}

func (f *fakeNetworkManager) CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	f.removed = append(f.removed, dnatCall{Protocol: protocol, DstPort: dstPort, TargetIP: targetIP, TargetPort: targetPort})
	return nil
}

const testNetworkType = "fake-test-net"
