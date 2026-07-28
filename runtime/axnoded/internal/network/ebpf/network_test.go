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
	mu           sync.Mutex
	ensureErr    error
	upsertErr    error
	deleteErr    error
	cleanupErr   error
	gcErr        error
	fullFallback map[string]bool
	localCompat  map[string]bool
	upserts      int
	deletes      int
	ensureCalls  int
	gcCalls      int
	gcPolicy     bpfnet.SNATGCPolicy
	status       bpfnet.Status
	statusErr    error
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

func (f *fakeController) NeedsSNATFallback() bool {
	return f.fullFallback["snat"]
}

func (f *fakeController) NeedsFullDNATFallback(protocol string) bool {
	return f.fullFallback[protocol]
}

func (f *fakeController) NeedsLocalhostCompat(protocol string) bool {
	return f.localCompat[protocol]
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

type fakeFallback struct {
	setupSNATCalls     int
	cleanupSNATCalls   int
	setupDNATCalls     int
	setupCompatCalls   int
	cleanupDNATCalls   int
	cleanupCompatCalls int
	setupDNATErr       error
	setupCompatErr     error
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

func (f *fakeFallback) SetupDNATCompatRule(string, uint16, string, uint16) error {
	f.setupCompatCalls++
	return f.setupCompatErr
}

func (f *fakeFallback) CleanupDNATRule(string, uint16, string, uint16) error {
	f.cleanupDNATCalls++
	return nil
}

func (f *fakeFallback) CleanupDNATCompatRule(string, uint16, string, uint16) error {
	f.cleanupCompatCalls++
	return nil
}

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

func TestSetupDNATRuleRollsBackControllerOnFallbackFailure(t *testing.T) {
	ctrl := &fakeController{fullFallback: map[string]bool{"tcp": true}}
	fallback := &fakeFallback{setupDNATErr: errors.New("boom")}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	err := manager.SetupDNATRule("tcp", 18080, "172.17.0.2", 80)
	if err == nil {
		t.Fatalf("expected error")
	}
	if ctrl.deletes != 1 {
		t.Fatalf("expected controller rollback, got %d delete calls", ctrl.deletes)
	}
}

func TestSetupSNATRulesUsesFallbackWhenEnabled(t *testing.T) {
	ctrl := &fakeController{fullFallback: map[string]bool{"snat": true}}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupSNATRules("172.17.0.1/16"); err != nil {
		t.Fatalf("setup snat rules: %v", err)
	}
	if ctrl.ensureCalls != 1 {
		t.Fatalf("expected 1 ensure call, got %d", ctrl.ensureCalls)
	}
	if fallback.setupSNATCalls != 1 {
		t.Fatalf("expected 1 fallback snat call, got %d", fallback.setupSNATCalls)
	}
}

func TestSetupSNATRulesSkipsFallbackWhenDataplaneIsReady(t *testing.T) {
	ctrl := &fakeController{fullFallback: map[string]bool{}}
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
	ctrl := &fakeController{fullFallback: map[string]bool{}}
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

func TestSetupDNATRuleUsesCompatOnlyWhenIngressDatapathIsReady(t *testing.T) {
	ctrl := &fakeController{localCompat: map[string]bool{"tcp": true}}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupDNATRule("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("setup dnat rule: %v", err)
	}
	if fallback.setupDNATCalls != 0 {
		t.Fatalf("expected PREROUTING/FORWARD fallback to stay disabled, got %d calls", fallback.setupDNATCalls)
	}
	if fallback.setupCompatCalls != 1 {
		t.Fatalf("expected localhost compat helper to be called once, got %d", fallback.setupCompatCalls)
	}
}

func TestSetupDNATRuleSkipsCompatWhenLocalhostEBPFPathIsReady(t *testing.T) {
	ctrl := &fakeController{
		fullFallback: map[string]bool{},
		localCompat:  map[string]bool{},
	}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupDNATRule("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("setup dnat rule: %v", err)
	}
	if fallback.setupDNATCalls != 0 {
		t.Fatalf("expected tcp PREROUTING/FORWARD fallback to stay disabled, got %d calls", fallback.setupDNATCalls)
	}
	if fallback.setupCompatCalls != 0 {
		t.Fatalf("expected tcp localhost compat helper to stay disabled, got %d calls", fallback.setupCompatCalls)
	}
}

func TestSetupDNATRuleSkipsFallbackForUDPWhenDatapathIsReady(t *testing.T) {
	ctrl := &fakeController{
		fullFallback: map[string]bool{},
		localCompat:  map[string]bool{},
	}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.SetupDNATRule("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("setup udp dnat rule: %v", err)
	}
	if fallback.setupDNATCalls != 0 {
		t.Fatalf("expected udp PREROUTING/FORWARD fallback to stay disabled, got %d calls", fallback.setupDNATCalls)
	}
	if fallback.setupCompatCalls != 0 {
		t.Fatalf("expected udp localhost compat helper to stay disabled, got %d calls", fallback.setupCompatCalls)
	}
}

func TestCleanupDNATRuleUsesFullFallbackForUDPWhenDatapathIsNotReady(t *testing.T) {
	ctrl := &fakeController{fullFallback: map[string]bool{"udp": true}}
	fallback := &fakeFallback{}
	manager := &BPFNetworkManager{controller: ctrl, fallback: fallback}

	if err := manager.CleanupDNATRule("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("cleanup udp dnat rule: %v", err)
	}
	if fallback.cleanupDNATCalls != 1 {
		t.Fatalf("expected udp cleanup to use full fallback once, got %d calls", fallback.cleanupDNATCalls)
	}
	if fallback.cleanupCompatCalls != 0 {
		t.Fatalf("expected udp cleanup to skip localhost compat, got %d calls", fallback.cleanupCompatCalls)
	}
}

func TestReconcileDNATRulesRemovesOrphansAndEnsuresDesiredState(t *testing.T) {
	ctrl := &fakeController{
		localCompat: map[string]bool{"tcp": true},
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
	if ctrl.deletes != 1 || fallback.cleanupCompatCalls != 1 {
		t.Fatalf("expected one orphan cleanup, deletes=%d compat_cleanups=%d", ctrl.deletes, fallback.cleanupCompatCalls)
	}
	if ctrl.upserts != 1 || fallback.setupCompatCalls != 1 {
		t.Fatalf("expected one desired rule setup, upserts=%d compat_setups=%d", ctrl.upserts, fallback.setupCompatCalls)
	}
}

func TestReconcileDNATRulesDoesNotUpsertOverFailedCleanup(t *testing.T) {
	ctrl := &fakeController{
		deleteErr:   errors.New("delete failed"),
		localCompat: map[string]bool{"tcp": true},
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
