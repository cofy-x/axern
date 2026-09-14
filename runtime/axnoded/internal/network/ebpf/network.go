package ebpf

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
)

type dataplaneController interface {
	EnsureAttached(ipRange string) error
	Cleanup() error
	UpsertService(protocol string, hostPort uint16, targetIP string, targetPort uint16) error
	DeleteService(protocol string, hostPort uint16, targetIP string, targetPort uint16) error
	CleanupStaleSNATMappings(policy bpfnet.SNATGCPolicy) (bpfnet.SNATGCResult, error)
	Status() (bpfnet.Status, error)
}

type controllerFactory func(cfg config.BPFNetConfig) (dataplaneController, error)

var (
	managerMu        sync.Mutex
	newControllerFor = defaultControllerFactory
)

type BPFNetworkManager struct {
	controller dataplaneController
	gcInterval time.Duration
	gcPolicy   bpfnet.SNATGCPolicy
	gcMu       sync.Mutex
	gcStop     chan struct{}
}

func (m *BPFNetworkManager) ProbeHealth(ipRange string) (networkmanager.Health, error) {
	if ipv6, err := isIPv6Range(ipRange); err != nil {
		return networkmanager.Health{}, err
	} else if ipv6 {
		return networkmanager.Health{}, ipv6UnsupportedError()
	}
	status, err := m.controller.Status()
	if err != nil {
		return networkmanager.Health{}, fmt.Errorf("read bpfnet dataplane status: %w", err)
	}
	return networkmanager.Health{
		PortForwardingReady:  status.State.TCReady && status.State.LocalhostPathReady,
		NativeDataplaneReady: status.State.TCReady && status.State.LocalhostPathReady,
	}, nil
}

func defaultControllerFactory(cfg config.BPFNetConfig) (dataplaneController, error) {
	controller := bpfnet.NewController(bpfnet.Config{
		UplinkDevices:      append([]string(nil), cfg.UplinkDevices...),
		PinPath:            cfg.PinPath,
		MapSize:            cfg.MapSize,
		SNATMapSize:        cfg.SNATMapSize,
		NativeRoutingCIDRs: append([]string(nil), cfg.NativeRoutingCIDRs...),
	})
	return controller, nil
}

func Configure(cfg config.BPFNetConfig) error {
	managerMu.Lock()
	defer managerMu.Unlock()

	controller, err := newControllerFor(cfg)
	if err != nil {
		return err
	}
	gcInterval, gcPolicy, err := snatGCSettings(cfg)
	if err != nil {
		return err
	}
	networkmanager.Register(config.NatBackendEBPF, &BPFNetworkManager{
		controller: controller,
		gcInterval: gcInterval,
		gcPolicy:   gcPolicy,
	})
	return nil
}

func (m *BPFNetworkManager) SetupSNATRules(ipRange string) error {
	if ipv6, err := isIPv6Range(ipRange); err != nil {
		return err
	} else if ipv6 {
		return ipv6UnsupportedError()
	}
	if err := m.controller.EnsureAttached(ipRange); err != nil {
		return err
	}
	m.startSNATGC()
	return nil
}

func (m *BPFNetworkManager) CleanupSNATRules(ipRange string) error {
	if ipv6, err := isIPv6Range(ipRange); err != nil {
		return err
	} else if ipv6 {
		return ipv6UnsupportedError()
	}
	m.stopSNATGC()
	return m.controller.Cleanup()
}

func snatGCSettings(cfg config.BPFNetConfig) (time.Duration, bpfnet.SNATGCPolicy, error) {
	interval, err := cfg.SNATGCIntervalDuration()
	if err != nil {
		return 0, bpfnet.SNATGCPolicy{}, fmt.Errorf("parse bpfnet snat gc interval: %w", err)
	}
	tcpIdle, err := cfg.SNATTCPIdleTimeoutDuration()
	if err != nil {
		return 0, bpfnet.SNATGCPolicy{}, fmt.Errorf("parse bpfnet snat tcp idle timeout: %w", err)
	}
	tcpClosing, err := cfg.SNATTCPClosingTimeoutDuration()
	if err != nil {
		return 0, bpfnet.SNATGCPolicy{}, fmt.Errorf("parse bpfnet snat tcp closing timeout: %w", err)
	}
	datagramIdle, err := cfg.SNATDatagramIdleTimeoutDuration()
	if err != nil {
		return 0, bpfnet.SNATGCPolicy{}, fmt.Errorf("parse bpfnet snat datagram idle timeout: %w", err)
	}
	return interval, bpfnet.SNATGCPolicy{
		TCPIdleTimeout:      tcpIdle,
		TCPClosingTimeout:   tcpClosing,
		DatagramIdleTimeout: datagramIdle,
	}, nil
}

func (m *BPFNetworkManager) startSNATGC() {
	if m.gcInterval <= 0 {
		return
	}
	m.gcMu.Lock()
	defer m.gcMu.Unlock()
	if m.gcStop != nil {
		return
	}
	stop := make(chan struct{})
	m.gcStop = stop
	go m.runSNATGC(stop)
}

func (m *BPFNetworkManager) stopSNATGC() {
	m.gcMu.Lock()
	defer m.gcMu.Unlock()
	if m.gcStop == nil {
		return
	}
	close(m.gcStop)
	m.gcStop = nil
}

func (m *BPFNetworkManager) runSNATGC(stop <-chan struct{}) {
	ticker := time.NewTicker(m.gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _ = m.controller.CleanupStaleSNATMappings(m.gcPolicy)
		case <-stop:
			return
		}
	}
}

func (m *BPFNetworkManager) SetupNetworkRulesForActivating(ip net.IP, envID string) error {
	if ip != nil && ip.To4() == nil {
		return ipv6UnsupportedError()
	}
	return nil
}

func (m *BPFNetworkManager) CleanupNetworkRulesForActivating(ip net.IP) error {
	if ip != nil && ip.To4() == nil {
		return ipv6UnsupportedError()
	}
	return nil
}

func (m *BPFNetworkManager) SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	if isIPv6Address(targetIP) {
		return ipv6UnsupportedError()
	}
	if err := m.controller.EnsureAttached(""); err != nil {
		return err
	}
	if err := m.controller.UpsertService(protocol, dstPort, targetIP, targetPort); err != nil {
		return fmt.Errorf("bpfnet upsert service: %w", err)
	}
	return nil
}

func (m *BPFNetworkManager) CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	if isIPv6Address(targetIP) {
		return ipv6UnsupportedError()
	}
	return m.controller.DeleteService(protocol, dstPort, targetIP, targetPort)
}

func (m *BPFNetworkManager) ReconcileDNATRules(desired []networkmanager.DNATRule) error {
	for _, rule := range desired {
		if isIPv6Address(rule.TargetIP) {
			return ipv6UnsupportedError()
		}
	}
	status, err := m.controller.Status()
	if err != nil {
		return fmt.Errorf("read bpfnet service state: %w", err)
	}

	desiredByKey := make(map[string]networkmanager.DNATRule, len(desired))
	for _, rule := range desired {
		desiredByKey[dnatRuleKey(rule.Protocol, rule.HostPort)] = rule
	}
	currentByKey := make(map[string]bpfnet.Service, len(status.Services))
	blockedKeys := make(map[string]struct{})
	var errs []error
	for _, current := range status.Services {
		key := dnatRuleKey(current.Protocol, current.HostPort)
		currentByKey[key] = current
		next, keep := desiredByKey[key]
		if keep && dnatRulesEqual(current, next) {
			continue
		}
		if err := m.CleanupDNATRule(current.Protocol, current.HostPort, current.TargetIP, current.TargetPort); err != nil {
			errs = append(errs, fmt.Errorf("remove orphaned bpfnet service %s: %w", key, err))
			blockedKeys[key] = struct{}{}
			continue
		}
		delete(currentByKey, key)
	}

	for key, rule := range desiredByKey {
		if _, blocked := blockedKeys[key]; blocked {
			continue
		}
		if current, ok := currentByKey[key]; ok && dnatRulesEqual(current, rule) {
			continue
		}
		if err := m.SetupDNATRule(rule.Protocol, rule.HostPort, rule.TargetIP, rule.TargetPort); err != nil {
			errs = append(errs, fmt.Errorf("ensure bpfnet service %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func isIPv6Range(ipRange string) (bool, error) {
	ipRange = strings.TrimSpace(ipRange)
	if ipRange == "" {
		return false, nil
	}
	prefix, err := netip.ParsePrefix(ipRange)
	if err != nil {
		return false, fmt.Errorf("parse sandbox IP range %q: %w", ipRange, err)
	}
	return prefix.Addr().Is6(), nil
}

func isIPv6Address(address string) bool {
	ip := net.ParseIP(strings.TrimSpace(address))
	return ip != nil && ip.To4() == nil
}

func ipv6UnsupportedError() error {
	return errors.New("ebpf network backend supports IPv4 only; select the iptables backend for an IPv6 sandbox range")
}

func dnatRuleKey(protocol string, hostPort uint16) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(protocol), hostPort)
}

func dnatRulesEqual(current bpfnet.Service, desired networkmanager.DNATRule) bool {
	return strings.EqualFold(current.Protocol, desired.Protocol) &&
		current.HostPort == desired.HostPort &&
		current.TargetIP == desired.TargetIP &&
		current.TargetPort == desired.TargetPort
}

func setControllerFactoryForTest(factory controllerFactory) {
	managerMu.Lock()
	defer managerMu.Unlock()
	newControllerFor = factory
}

func resetControllerFactoryForTest() {
	managerMu.Lock()
	defer managerMu.Unlock()
	newControllerFor = defaultControllerFactory
}
