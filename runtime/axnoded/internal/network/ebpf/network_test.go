package ebpf

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
)

type fakeController struct {
	mu          sync.Mutex
	ensureErr   error
	upsertErr   error
	deleteErr   error
	cleanupErr  error
	gcErr       error
	upserts     int
	deletes     int
	ensureCalls int
	gcCalls     int
	gcPolicy    bpfnet.SNATGCPolicy
	status      bpfnet.Status
	statusErr   error
}

func (f *fakeController) EnsureAttached(string) error {
	f.ensureCalls++
	return f.ensureErr
}

func (f *fakeController) Cleanup() error {
	return f.cleanupErr
}

func (f *fakeController) UpsertService(string, uint16, string, uint16) error {
	f.upserts++
	return f.upsertErr
}

func (f *fakeController) DeleteService(string, uint16, string, uint16) error {
	f.deletes++
	return f.deleteErr
}

func (f *fakeController) CleanupStaleSNATMappings(policy bpfnet.SNATGCPolicy) (bpfnet.SNATGCResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gcCalls++
	f.gcPolicy = policy
	return bpfnet.SNATGCResult{}, f.gcErr
}

func (f *fakeController) Status() (bpfnet.Status, error) {
	return f.status, f.statusErr
}

func (f *fakeController) gcSnapshot() (int, bpfnet.SNATGCPolicy) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gcCalls, f.gcPolicy
}

func TestProbeHealthRejectsMissingLocalhostPath(t *testing.T) {
	manager := &BPFNetworkManager{controller: &fakeController{status: bpfnet.Status{State: bpfnet.DataplaneState{
		TCReady:            true,
		LastLocalhostError: "read host netns cookie: protocol not available",
	}}}}

	health, err := manager.ProbeHealth("")
	if err != nil {
		t.Fatalf("ProbeHealth() error = %v", err)
	}
	if health.PortForwardingReady || health.NativeDataplaneReady {
		t.Fatalf("ProbeHealth() = %#v, want incomplete dataplane unavailable", health)
	}
}

func TestProbeHealthReportsFailedAttachAsUnavailable(t *testing.T) {
	manager := &BPFNetworkManager{controller: &fakeController{status: bpfnet.Status{State: bpfnet.DataplaneState{
		LastAttachError:  "tc attach failed",
		LastTCProbeError: "tc probe failed",
	}}}}

	health, err := manager.ProbeHealth("")
	if err != nil {
		t.Fatalf("ProbeHealth() error = %v", err)
	}
	if health.PortForwardingReady || health.NativeDataplaneReady {
		t.Fatalf("ProbeHealth() = %#v, want unavailable dataplane", health)
	}
}

func TestProbeHealthReturnsStatusReadError(t *testing.T) {
	manager := &BPFNetworkManager{controller: &fakeController{statusErr: errors.New("status unavailable")}}

	if _, err := manager.ProbeHealth(""); err == nil {
		t.Fatal("ProbeHealth() error = nil, want status read error")
	}
}

type fakeFallback struct {
	setupSNATCalls   int
	cleanupSNATCalls int
	setupDNATCalls   int
	cleanupDNATCalls int
	setupDNATErr     error
	health           networkmanager.Health
}

func (f *fakeFallback) SetupSNATRules(string) error {
	f.setupSNATCalls++
	return nil
}

func (f *fakeFallback) CleanupSNATRules(string) error {
	f.cleanupSNATCalls++
	return nil
}

func (f *fakeFallback) SetupNetworkRulesForActivating(net.IP, string) error { return nil }
func (f *fakeFallback) CleanupNetworkRulesForActivating(net.IP) error       { return nil }

func (f *fakeFallback) SetupDNATRule(string, uint16, string, uint16) error {
	f.setupDNATCalls++
	return f.setupDNATErr
}

func (f *fakeFallback) CleanupDNATRule(string, uint16, string, uint16) error {
	f.cleanupDNATCalls++
	return nil
}

func (f *fakeFallback) ProbeHealth(string) (networkmanager.Health, error) { return f.health, nil }

func TestConfigureRegistersBackend(t *testing.T) {
	resetControllerFactoryForTest()
	t.Cleanup(resetControllerFactoryForTest)
	setControllerFactoryForTest(func(cfg config.BPFNetConfig) (dataplaneController, error) {
		if cfg.PinPath != "/pins" {
			t.Fatalf("unexpected pin path: %q", cfg.PinPath)
		}
		return &fakeController{}, nil
	})

	if err := Configure(config.BPFNetConfig{PinPath: "/pins"}); err != nil {
		t.Fatalf("configure ebpf backend: %v", err)
	}
	if _, ok := networkmanager.NetworkManagers[config.NatBackendEBPF]; !ok {
		t.Fatalf("expected ebpf backend to be registered")
	}
}

func TestIPv6UsesBridgeCompatibilityWithoutAttachingBPF(t *testing.T) {
	ctrl := &fakeController{}
	fallback := &fakeFallback{health: networkmanager.Health{PortForwardingReady: true, NativeDataplaneReady: true}}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupSNATRules("fd31::1/64"); err != nil {
		t.Fatalf("setup IPv6 SNAT rules: %v", err)
	}
	if ctrl.ensureCalls != 0 || fallback.setupSNATCalls != 1 || !manager.ipv6Compat.Load() {
		t.Fatalf("unexpected IPv6 path: ensure=%d fallback=%d compat=%v", ctrl.ensureCalls, fallback.setupSNATCalls, manager.ipv6Compat.Load())
	}
	health, err := manager.ProbeHealth("fd31::1/64")
	if err != nil || !health.NativeDataplaneReady {
		t.Fatalf("IPv6 compatibility health = %#v, %v", health, err)
	}
	if err := manager.SetupDNATRule("tcp", 8443, "fd31::2", 443); err != nil {
		t.Fatalf("setup IPv6 DNAT: %v", err)
	}
	if ctrl.upserts != 0 || fallback.setupDNATCalls != 1 {
		t.Fatalf("unexpected IPv6 DNAT path: upserts=%d fallback=%d", ctrl.upserts, fallback.setupDNATCalls)
	}
}

func TestSetupSNATRulesUsesBPFDataplane(t *testing.T) {
	ctrl := &fakeController{}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupSNATRules("172.17.0.1/16"); err != nil {
		t.Fatalf("setup snat rules: %v", err)
	}
	if ctrl.ensureCalls != 1 {
		t.Fatalf("expected 1 ensure call, got %d", ctrl.ensureCalls)
	}
	if fallback.setupSNATCalls != 0 {
		t.Fatalf("expected fallback snat helper to stay idle, got %d calls", fallback.setupSNATCalls)
	}
}

func TestSetupSNATRulesStartsSNATGCWhenDataplaneIsReady(t *testing.T) {
	ctrl := &fakeController{}
	fallback := &fakeFallback{}
	policy := bpfnet.SNATGCPolicy{
		TCPIdleTimeout:      time.Minute,
		TCPClosingTimeout:   time.Second,
		DatagramIdleTimeout: 30 * time.Second,
	}
	manager := &BPFNetworkManager{
		controller: ctrl,
		fallback:   fallback,
		gcInterval: time.Millisecond,
		gcPolicy:   policy,
	}
	defer manager.stopSNATGC()

	if err := manager.SetupSNATRules("172.17.0.1/16"); err != nil {
		t.Fatalf("setup snat rules: %v", err)
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	calls, gotPolicy := ctrl.gcSnapshot()
	for calls == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		calls, gotPolicy = ctrl.gcSnapshot()
	}
	if calls == 0 {
		t.Fatalf("expected snat gc to run")
	}
	if gotPolicy != policy {
		t.Fatalf("expected gc policy %#v, got %#v", policy, gotPolicy)
	}
}

func TestSNATGCSettingsParsesDurations(t *testing.T) {
	interval, policy, err := snatGCSettings(config.BPFNetConfig{
		SNATGCInterval:          "2s",
		SNATTCPIdleTimeout:      "3m",
		SNATTCPClosingTimeout:   "4s",
		SNATDatagramIdleTimeout: "5s",
	})
	if err != nil {
		t.Fatalf("snat gc settings: %v", err)
	}
	if interval != 2*time.Second || policy.TCPIdleTimeout != 3*time.Minute || policy.TCPClosingTimeout != 4*time.Second || policy.DatagramIdleTimeout != 5*time.Second {
		t.Fatalf("unexpected snat gc settings: interval=%v policy=%#v", interval, policy)
	}
}

func TestSetupDNATRuleUsesOnlyBPFDataplane(t *testing.T) {
	ctrl := &fakeController{}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupDNATRule("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("setup dnat rule: %v", err)
	}
	if fallback.setupDNATCalls != 0 {
		t.Fatalf("expected tcp PREROUTING/FORWARD fallback to stay disabled, got %d calls", fallback.setupDNATCalls)
	}
}

func TestSetupDNATRuleSkipsFallbackForUDPWhenDatapathIsReady(t *testing.T) {
	ctrl := &fakeController{}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupDNATRule("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("setup udp dnat rule: %v", err)
	}
	if fallback.setupDNATCalls != 0 {
		t.Fatalf("expected udp PREROUTING/FORWARD fallback to stay disabled, got %d calls", fallback.setupDNATCalls)
	}
}

func TestReconcileDNATRulesRemovesOrphansAndEnsuresDesiredState(t *testing.T) {
	ctrl := &fakeController{
		status: bpfnet.Status{Services: []bpfnet.Service{
			{Protocol: "tcp", HostPort: 18080, TargetIP: "172.17.0.2", TargetPort: 80},
			{Protocol: "tcp", HostPort: 19090, TargetIP: "172.17.0.9", TargetPort: 90},
		}},
	}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	err := manager.ReconcileDNATRules([]networkmanager.DNATRule{
		{Protocol: "tcp", HostPort: 18080, TargetIP: "172.17.0.2", TargetPort: 80},
		{Protocol: "tcp", HostPort: 17070, TargetIP: "172.17.0.7", TargetPort: 70},
	})
	if err != nil {
		t.Fatalf("reconcile dnat rules: %v", err)
	}
	if ctrl.deletes != 1 {
		t.Fatalf("expected one orphan cleanup, deletes=%d", ctrl.deletes)
	}
	if ctrl.upserts != 1 {
		t.Fatalf("expected one desired rule setup, upserts=%d", ctrl.upserts)
	}
}

func TestReconcileDNATRulesDoesNotUpsertOverFailedCleanup(t *testing.T) {
	ctrl := &fakeController{
		deleteErr: errors.New("delete failed"),
		status: bpfnet.Status{Services: []bpfnet.Service{{
			Protocol: "tcp", HostPort: 18080, TargetIP: "172.17.0.2", TargetPort: 80,
		}}},
	}
	manager := &BPFNetworkManager{controller: ctrl, fallback: &fakeFallback{}}

	err := manager.ReconcileDNATRules([]networkmanager.DNATRule{{
		Protocol: "tcp", HostPort: 18080, TargetIP: "172.17.0.9", TargetPort: 80,
	}})
	if err == nil {
		t.Fatal("expected cleanup failure")
	}
	if ctrl.upserts != 0 {
		t.Fatalf("expected conflicting upsert to be skipped, got %d", ctrl.upserts)
	}
}
