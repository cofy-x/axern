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
		NativeDataplaneReady: status.State.TCReady,
	}, nil
}

func defaultControllerFactory(cfg config.BPFNetConfig) (dataplaneController, error) {
	controller := bpfnet.NewController(bpfnet.Config{
		UplinkDevices:      append([]string(nil), cfg.UplinkDevices...),
		PinPath:            cfg.PinPath,
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

func ipv6UnsupportedError() error {
	return errors.New("ebpf network backend supports IPv4 only; select the iptables backend for an IPv6 sandbox range")
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
