package ebpf

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/internal/network/bridge"
)

type dataplaneController interface {
	EnsureAttached(ipRange string) error
	Cleanup() error
	UpsertService(protocol string, hostPort uint16, targetIP string, targetPort uint16) error
	DeleteService(protocol string, hostPort uint16, targetIP string, targetPort uint16) error
	NeedsSNATFallback() bool
	NeedsFullDNATFallback(protocol string) bool
	NeedsLocalhostCompat(protocol string) bool
	CleanupStaleSNATMappings(policy bpfnet.SNATGCPolicy) (bpfnet.SNATGCResult, error)
	Status() (bpfnet.Status, error)
}

type controllerFactory func(cfg config.BPFNetConfig) (dataplaneController, error)

type dnatCompatFallback interface {
	SetupDNATCompatRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error
	CleanupDNATCompatRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error
}

var (
	managerMu        sync.Mutex
	newControllerFor = defaultControllerFactory
)

type BPFNetworkManager struct {
	controller dataplaneController
	fallback   networkmanager.NetworkManager
	gcInterval time.Duration
	gcPolicy   bpfnet.SNATGCPolicy
	gcMu       sync.Mutex
	gcStop     chan struct{}
}

func (m *BPFNetworkManager) ProbeHealth(string) (networkmanager.Health, error) {
	status, err := m.controller.Status()
	if err != nil {
		return networkmanager.Health{}, fmt.Errorf("read bpfnet dataplane status: %w", err)
	}
	// Persisted readiness is authoritative. In particular, failure of the
	// optional localhost cgroup path records LastLocalhostError while TC ingress
	// and egress remain healthy, and full iptables fallback can still provide
	// port forwarding without satisfying the native bpfnet capability.
	return networkmanager.Health{
		PortForwardingReady:  status.State.TCReady || status.State.FullFallback,
		NativeDataplaneReady: status.State.TCReady,
	}, nil
}

func defaultControllerFactory(cfg config.BPFNetConfig) (dataplaneController, error) {
	controller := bpfnet.NewController(bpfnet.Config{
		UplinkDevices:      append([]string(nil), cfg.UplinkDevices...),
		PinPath:            cfg.PinPath,
		MapSize:            cfg.MapSize,
		SNATMapSize:        cfg.SNATMapSize,
		LocalOutCompat:     cfg.LocalOutCompat,
		NativeRoutingCIDRs: append([]string(nil), cfg.NativeRoutingCIDRs...),
		IptablesFallback:   cfg.IptablesFallback,
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
		fallback:   &bridge.BridgeNetworkManager{},
		gcInterval: gcInterval,
		gcPolicy:   gcPolicy,
	})
	return nil
}

func (m *BPFNetworkManager) SetupSNATRules(ipRange string) error {
	if err := m.controller.EnsureAttached(ipRange); err != nil {
		return err
	}
	if m.controller.NeedsSNATFallback() {
		m.stopSNATGC()
		return m.fallback.SetupSNATRules(ipRange)
	}
	m.startSNATGC()
	return nil
}

func (m *BPFNetworkManager) CleanupSNATRules(ipRange string) error {
	var firstErr error
	m.stopSNATGC()
	if m.controller.NeedsSNATFallback() {
		if err := m.fallback.CleanupSNATRules(ipRange); err != nil {
			firstErr = err
		}
	}
	if err := m.controller.Cleanup(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
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
	if m.controller.NeedsSNATFallback() {
		return m.fallback.SetupNetworkRulesForActivating(ip, envID)
	}
	return nil
}

func (m *BPFNetworkManager) CleanupNetworkRulesForActivating(ip net.IP) error {
	if m.controller.NeedsSNATFallback() {
		return m.fallback.CleanupNetworkRulesForActivating(ip)
	}
	return nil
}

func (m *BPFNetworkManager) SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	if err := m.controller.EnsureAttached(""); err != nil {
		return err
	}
	if err := m.controller.UpsertService(protocol, dstPort, targetIP, targetPort); err != nil {
		return fmt.Errorf("bpfnet upsert service: %w", err)
	}
	if m.controller.NeedsFullDNATFallback(protocol) {
		if err := m.fallback.SetupDNATRule(protocol, dstPort, targetIP, targetPort); err != nil {
			_ = m.controller.DeleteService(protocol, dstPort, targetIP, targetPort)
			return err
		}
		return nil
	}
	if m.controller.NeedsLocalhostCompat(protocol) {
		compat, ok := m.fallback.(dnatCompatFallback)
		if !ok {
			_ = m.controller.DeleteService(protocol, dstPort, targetIP, targetPort)
			return fmt.Errorf("fallback network manager does not support localhost DNAT compatibility")
		}
		if err := compat.SetupDNATCompatRule(protocol, dstPort, targetIP, targetPort); err != nil {
			_ = m.controller.DeleteService(protocol, dstPort, targetIP, targetPort)
			return err
		}
	}
	return nil
}

func (m *BPFNetworkManager) CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	var firstErr error
	if m.controller.NeedsFullDNATFallback(protocol) {
		if err := m.fallback.CleanupDNATRule(protocol, dstPort, targetIP, targetPort); err != nil {
			firstErr = err
		}
	} else if m.controller.NeedsLocalhostCompat(protocol) {
		compat, ok := m.fallback.(dnatCompatFallback)
		if !ok {
			firstErr = fmt.Errorf("fallback network manager does not support localhost DNAT compatibility")
		} else if err := compat.CleanupDNATCompatRule(protocol, dstPort, targetIP, targetPort); err != nil {
			firstErr = err
		}
	}
	if err := m.controller.DeleteService(protocol, dstPort, targetIP, targetPort); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (m *BPFNetworkManager) ReconcileDNATRules(desired []networkmanager.DNATRule) error {
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
