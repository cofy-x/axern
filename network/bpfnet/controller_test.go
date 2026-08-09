package bpfnet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeDataplane struct {
	attachment  dataplaneAttachment
	ensureErr   error
	ipRange     string
	nativeCIDRs []string
	services    []Service
	upserts     []Service
	deletes     []Service
	gcPolicy    SNATGCPolicy
	gcResult    SNATGCResult
	gcErr       error
}

func (f *fakeDataplane) EnsureAttached(_ []string, ipRange string, nativeCIDRs []string, services []Service) (dataplaneAttachment, error) {
	f.ipRange = ipRange
	f.nativeCIDRs = append([]string(nil), nativeCIDRs...)
	f.services = append([]Service(nil), services...)
	return f.attachment, f.ensureErr
}

func (f *fakeDataplane) UpsertService(service Service) error {
	f.upserts = append(f.upserts, service)
	return nil
}

func (f *fakeDataplane) DeleteService(service Service) error {
	f.deletes = append(f.deletes, service)
	return nil
}

func (f *fakeDataplane) CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error) {
	f.gcPolicy = policy
	return f.gcResult, f.gcErr
}

func TestConfigWithDefaults(t *testing.T) {
	cfg := (Config{}).WithDefaults()
	if cfg.PinPath != DefaultPinPath {
		t.Fatalf("expected default pin path %q, got %q", DefaultPinPath, cfg.PinPath)
	}
	if cfg.StatePath != DefaultStatePath {
		t.Fatalf("expected default state path %q, got %q", DefaultStatePath, cfg.StatePath)
	}
	if cfg.MapSize != DefaultMapSize {
		t.Fatalf("expected default map size %d, got %d", DefaultMapSize, cfg.MapSize)
	}
	if cfg.SNATMapSize != DefaultSNATMapSize {
		t.Fatalf("expected default snat map size %d, got %d", DefaultSNATMapSize, cfg.SNATMapSize)
	}
}

func TestConfigWithDefaultsUsesWritableStatePathOutsideBPFFS(t *testing.T) {
	root := t.TempDir()
	cfg := (Config{PinPath: filepath.Join(root, "pins")}).WithDefaults()
	if cfg.StatePath != filepath.Join(root, "pins") {
		t.Fatalf("expected state path to follow pin path, got %q", cfg.StatePath)
	}
}

func TestControllerUpsertAndDeleteService(t *testing.T) {
	dp := &fakeDataplane{
		attachment: dataplaneAttachment{
			LocalAddresses:   []string{"127.0.0.1", "192.168.1.9"},
			LocalhostTCPDNAT: true,
		},
	}
	newDataplane = func(Config, commandRunner) dataplane { return dp }
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{
		PinPath:          t.TempDir(),
		UplinkDevices:    []string{"eth0"},
		LocalOutCompat:   true,
		IptablesFallback: true,
	})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	if err := ctrl.EnsureAttached("172.17.0.1/16"); err != nil {
		t.Fatalf("ensure attached: %v", err)
	}
	if err := ctrl.UpsertService("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("upsert service: %v", err)
	}
	if err := ctrl.UpsertService("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("upsert udp service: %v", err)
	}

	status, err := ctrl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(status.Services))
	}
	if status.State.Mode != ModeIngressTCPUDPDNATEgressSNATLocalhostTCP {
		t.Fatalf("unexpected dataplane mode %q", status.State.Mode)
	}
	if !status.State.EgressSNAT {
		t.Fatalf("expected egress SNAT mode to be enabled")
	}
	if !status.State.TCReady {
		t.Fatalf("expected tc dataplane to be recorded as ready")
	}
	if !status.State.IngressUDPDNAT {
		t.Fatalf("expected ingress udp dnat mode to be enabled")
	}
	if !status.State.LocalhostPathReady {
		t.Fatalf("expected localhost path to be recorded as ready")
	}
	if status.State.FullFallback {
		t.Fatalf("expected full fallback to stay disabled after successful attach")
	}
	if ctrl.NeedsSNATFallback() {
		t.Fatalf("expected successful attach to disable SNAT fallback")
	}
	if ctrl.NeedsFullDNATFallback("udp") {
		t.Fatalf("expected udp ingress to stay on the dataplane when attach succeeded")
	}
	if ctrl.NeedsLocalhostCompat("udp") {
		t.Fatalf("expected udp localhost compat to stay disabled")
	}
	if ctrl.NeedsLocalhostCompat("tcp") {
		t.Fatalf("expected localhost tcp compat to stay disabled when localhost ebpf path is ready")
	}
	if len(dp.upserts) != 2 {
		t.Fatalf("expected tcp+udp services to be synced into dataplane, got %d upserts", len(dp.upserts))
	}

	if err := ctrl.DeleteService("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("delete service: %v", err)
	}
	if err := ctrl.DeleteService("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("delete udp service: %v", err)
	}
	status, err = ctrl.Status()
	if err != nil {
		t.Fatalf("status after delete: %v", err)
	}
	if len(status.Services) != 0 {
		t.Fatalf("expected 0 services, got %d", len(status.Services))
	}
	if len(dp.deletes) != 2 {
		t.Fatalf("expected tcp+udp services to be removed from dataplane, got %d deletes", len(dp.deletes))
	}
}

func TestControllerDetectsServiceConflict(t *testing.T) {
	ctrl := NewController(Config{
		PinPath:          t.TempDir(),
		UplinkDevices:    []string{"eth0"},
		LocalOutCompat:   true,
		IptablesFallback: true,
	})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	if err := ctrl.UpsertService("tcp", 18080, "172.17.0.2", 80); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := ctrl.UpsertService("tcp", 18080, "172.17.0.3", 8080); err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestControllerResolvesDefaultRouteDevice(t *testing.T) {
	ctrl := NewController(Config{PinPath: t.TempDir()})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("default via 192.168.1.1 dev en0 proto dhcp src 192.168.1.9\n"), nil
	}

	devs, err := ctrl.resolveUplinks()
	if err != nil {
		t.Fatalf("resolve uplinks: %v", err)
	}
	if len(devs) != 1 || devs[0] != "en0" {
		t.Fatalf("unexpected uplinks: %#v", devs)
	}
}

func TestControllerFallsBackWhenDataplaneAttachFails(t *testing.T) {
	newDataplane = func(Config, commandRunner) dataplane {
		return &fakeDataplane{
			ensureErr: markTCProbeError(errors.New("attach failed")),
		}
	}
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{
		PinPath:          t.TempDir(),
		UplinkDevices:    []string{"eth0"},
		IptablesFallback: true,
	})

	if err := ctrl.EnsureAttached("172.17.0.1/16"); err != nil {
		t.Fatalf("ensure attached with fallback: %v", err)
	}
	if !ctrl.NeedsFullDNATFallback("tcp") {
		t.Fatalf("expected tcp ingress to fall back to iptables after attach failure")
	}
	if !ctrl.NeedsFullDNATFallback("udp") {
		t.Fatalf("expected udp ingress to fall back to iptables after attach failure")
	}

	status, err := ctrl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !ctrl.NeedsSNATFallback() {
		t.Fatalf("expected attach failure to keep SNAT fallback enabled")
	}
	if status.State.Mode != ModeIPTablesFullFallback {
		t.Fatalf("unexpected fallback mode %q", status.State.Mode)
	}
	if status.State.TCReady {
		t.Fatalf("expected tc dataplane to stay disabled in full fallback")
	}
	if !status.State.FullFallback {
		t.Fatalf("expected full fallback state to be recorded")
	}
	if status.State.LastAttachError == "" {
		t.Fatalf("expected attach error to be recorded in state")
	}
	if status.State.LastTCProbeError == "" {
		t.Fatalf("expected tc probe error to be recorded in state")
	}
}

func TestControllerRecordsUDPFallbackWhenDataplaneIsNotReady(t *testing.T) {
	ctrl := NewController(Config{
		PinPath:       t.TempDir(),
		UplinkDevices: []string{"eth0"},
	})

	if err := ctrl.UpsertService("udp", 15353, "172.17.0.3", 1053); err != nil {
		t.Fatalf("upsert udp service without dataplane: %v", err)
	}

	status, err := ctrl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Services) != 1 || status.Services[0].Protocol != "udp" {
		t.Fatalf("expected one udp service, got %#v", status.Services)
	}
	if status.Stats.Fallbacks != 1 {
		t.Fatalf("expected one fallback accounting event, got %d", status.Stats.Fallbacks)
	}
}

func TestControllerPreservesIPRangeAcrossCompatEnsure(t *testing.T) {
	dp := &fakeDataplane{
		attachment: dataplaneAttachment{LocalAddresses: []string{"127.0.0.1", "192.168.1.9"}, LocalhostTCPDNAT: true},
	}
	newDataplane = func(Config, commandRunner) dataplane { return dp }
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{
		PinPath:       t.TempDir(),
		UplinkDevices: []string{"eth0"},
	})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	if err := ctrl.EnsureAttached("172.17.0.1/16"); err != nil {
		t.Fatalf("first ensure attached: %v", err)
	}
	dp.ipRange = ""

	if err := ctrl.EnsureAttached(""); err != nil {
		t.Fatalf("second ensure attached: %v", err)
	}
	if dp.ipRange != "172.17.0.1/16" {
		t.Fatalf("expected persisted ip range to be reused, got %q", dp.ipRange)
	}
}

func TestControllerCleanupStaleSNATMappingsDelegatesToDataplane(t *testing.T) {
	dp := &fakeDataplane{
		gcResult: SNATGCResult{FwdScanned: 10, FwdDeleted: 3, RevScanned: 20, RevDeleted: 6},
	}
	newDataplane = func(Config, commandRunner) dataplane { return dp }
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{PinPath: t.TempDir()})
	policy := SNATGCPolicy{
		TCPIdleTimeout:      5,
		TCPClosingTimeout:   2,
		DatagramIdleTimeout: 3,
	}
	result, err := ctrl.CleanupStaleSNATMappings(policy)
	if err != nil {
		t.Fatalf("cleanup stale snat mappings: %v", err)
	}
	if dp.gcPolicy != policy {
		t.Fatalf("expected policy %#v, got %#v", policy, dp.gcPolicy)
	}
	if result.FwdDeleted != 3 || result.RevDeleted != 6 {
		t.Fatalf("unexpected gc result: %#v", result)
	}
}

func TestControllerFallsBackToLocalCompatWhenLocalhostAttachIsUnavailable(t *testing.T) {
	dp := &fakeDataplane{
		attachment: dataplaneAttachment{
			LocalAddresses:       []string{"127.0.0.1", "192.168.1.9"},
			LocalhostTCPDNAT:     false,
			LocalhostAttachError: "missing cgroup v2 root",
		},
	}
	newDataplane = func(Config, commandRunner) dataplane { return dp }
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{
		PinPath:          t.TempDir(),
		UplinkDevices:    []string{"eth0"},
		LocalOutCompat:   true,
		IptablesFallback: true,
	})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	if err := ctrl.EnsureAttached("172.17.0.1/16"); err != nil {
		t.Fatalf("ensure attached: %v", err)
	}
	if !ctrl.NeedsLocalhostCompat("tcp") {
		t.Fatalf("expected localhost compat to stay enabled when localhost ebpf attach is unavailable")
	}

	status, err := ctrl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State.Mode != ModeIngressTCPUDPDNATEgressSNATLocalCompat {
		t.Fatalf("unexpected compat mode %q", status.State.Mode)
	}
	if !status.State.TCReady {
		t.Fatalf("expected tc dataplane to stay ready during localhost compat fallback")
	}
	if status.State.LocalhostTCPDNAT {
		t.Fatalf("expected localhost tcp dnat to be recorded as disabled")
	}
	if !status.State.LocalhostCompat {
		t.Fatalf("expected localhost compat fallback to be recorded")
	}
	if status.State.LastLocalhostError == "" {
		t.Fatalf("expected localhost attach error to be recorded")
	}
	if status.State.LastAttachError != "" {
		t.Fatalf("localhost compatibility error was also recorded as a dataplane attach failure: %q", status.State.LastAttachError)
	}
}

func TestStatusAggregatesKernelAndAttachmentObservation(t *testing.T) {
	originalKernel := collectKernelStats
	originalSNATMaps := collectSNATMapStats
	originalAttachment := collectAttachmentReadiness
	collectKernelStats = func(Config) KernelStats {
		return KernelStats{
			AttachSuccesses:                    1,
			SNATHits:                           3,
			SNATFwdHits:                        11,
			SNATMappingsProgrammed:             13,
			SNATAllocCollisions:                5,
			SNATFallbackHits:                   2,
			SNATAllocExhausted:                 29,
			SNATTCPNonSynMisses:                31,
			SNATTCPNonSynMissFINs:              3,
			SNATTCPNonSynMissRSTs:              5,
			SNATTCPNonSynMissACKs:              19,
			SNATTCPNonSynMissOther:             4,
			SNATFullCloseReclaims:              37,
			SNATFullCloseMarks:                 41,
			SNATTCPFullCloseDeletes:            43,
			SNATTCPFullCloseDeletesFwd:         17,
			SNATTCPFullCloseDeletesRev:         26,
			SNATTCPNonSynMissFwdLookups:        47,
			SNATTCPNonSynMissFwdHostMismatches: 2,
			SNATTCPReverseMisses:               53,
			SNATTCPReverseMissSynACKs:          23,
			SNATTCPReverseMissFINs:             5,
			SNATTCPReverseMissRSTs:             7,
			SNATTCPReverseMissACKs:             11,
			SNATTCPReverseMissOther:            13,
			LocalhostConnectHits:               7,
		}
	}
	collectSNATMapStats = func(Config) SNATMapStats {
		return SNATMapStats{
			FwdEntries:             10,
			FwdTCPEntries:          3,
			FwdUDPEntries:          5,
			FwdICMPEntries:         2,
			FwdActiveEntries:       4,
			FwdClosingEntries:      6,
			FwdOrigClosingEntries:  2,
			FwdReplyClosingEntries: 1,
			FwdFullClosingEntries:  3,
			RevEntries:             12,
			RevTCPEntries:          4,
			RevUDPEntries:          6,
			RevICMPEntries:         2,
			RevActiveEntries:       5,
			RevClosingEntries:      7,
			RevOrigClosingEntries:  2,
			RevReplyClosingEntries: 2,
			RevFullClosingEntries:  3,
			RevReverseEntries:      9,
			RevAliasEntries:        3,
			TranslatedPortsUsed:    7,
			UDPTranslatedPortsUsed: 5,
		}
	}
	collectAttachmentReadiness = func(_ Config, state DataplaneState) AttachmentReadiness {
		return AttachmentReadiness{
			UplinkDevices:          append([]string(nil), state.UplinkDevices...),
			LocalAddresses:         []string{"127.0.0.1", "192.168.1.9"},
			IngressTCAttached:      true,
			EgressTCAttached:       true,
			LocalhostLinksAttached: true,
			PinnedMapsReady:        true,
			PinnedProgramsReady:    true,
		}
	}
	t.Cleanup(func() {
		collectKernelStats = originalKernel
		collectSNATMapStats = originalSNATMaps
		collectAttachmentReadiness = originalAttachment
	})

	dp := &fakeDataplane{
		attachment: dataplaneAttachment{
			LocalAddresses:   []string{"127.0.0.1", "192.168.1.9"},
			LocalhostTCPDNAT: true,
		},
	}
	newDataplane = func(Config, commandRunner) dataplane { return dp }
	t.Cleanup(func() {
		newDataplane = defaultDataplaneFactory
	})

	ctrl := NewController(Config{
		PinPath:       t.TempDir(),
		UplinkDevices: []string{"eth0"},
	})
	ctrl.run = func(name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	if err := ctrl.EnsureAttached("172.17.0.1/16"); err != nil {
		t.Fatalf("ensure attached: %v", err)
	}

	status, err := ctrl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	assertKernelStat(t, "AttachSuccesses", status.Kernel.AttachSuccesses, 1)
	assertKernelStat(t, "SNATHits", status.Kernel.SNATHits, 3)
	assertKernelStat(t, "SNATFwdHits", status.Kernel.SNATFwdHits, 11)
	assertKernelStat(t, "SNATMappingsProgrammed", status.Kernel.SNATMappingsProgrammed, 13)
	assertKernelStat(t, "SNATAllocCollisions", status.Kernel.SNATAllocCollisions, 5)
	assertKernelStat(t, "SNATFallbackHits", status.Kernel.SNATFallbackHits, 2)
	assertKernelStat(t, "SNATAllocExhausted", status.Kernel.SNATAllocExhausted, 29)
	assertKernelStat(t, "SNATTCPNonSynMisses", status.Kernel.SNATTCPNonSynMisses, 31)
	assertKernelStat(t, "SNATTCPNonSynMissFINs", status.Kernel.SNATTCPNonSynMissFINs, 3)
	assertKernelStat(t, "SNATTCPNonSynMissRSTs", status.Kernel.SNATTCPNonSynMissRSTs, 5)
	assertKernelStat(t, "SNATTCPNonSynMissACKs", status.Kernel.SNATTCPNonSynMissACKs, 19)
	assertKernelStat(t, "SNATTCPNonSynMissOther", status.Kernel.SNATTCPNonSynMissOther, 4)
	assertKernelStat(t, "SNATFullCloseReclaims", status.Kernel.SNATFullCloseReclaims, 37)
	assertKernelStat(t, "SNATFullCloseMarks", status.Kernel.SNATFullCloseMarks, 41)
	assertKernelStat(t, "SNATTCPFullCloseDeletes", status.Kernel.SNATTCPFullCloseDeletes, 43)
	assertKernelStat(t, "SNATTCPFullCloseDeletesFwd", status.Kernel.SNATTCPFullCloseDeletesFwd, 17)
	assertKernelStat(t, "SNATTCPFullCloseDeletesRev", status.Kernel.SNATTCPFullCloseDeletesRev, 26)
	assertKernelStat(t, "SNATTCPNonSynMissFwdLookups", status.Kernel.SNATTCPNonSynMissFwdLookups, 47)
	assertKernelStat(t, "SNATTCPNonSynMissFwdHostMismatches", status.Kernel.SNATTCPNonSynMissFwdHostMismatches, 2)
	assertKernelStat(t, "SNATTCPReverseMisses", status.Kernel.SNATTCPReverseMisses, 53)
	assertKernelStat(t, "SNATTCPReverseMissSynACKs", status.Kernel.SNATTCPReverseMissSynACKs, 23)
	assertKernelStat(t, "SNATTCPReverseMissFINs", status.Kernel.SNATTCPReverseMissFINs, 5)
	assertKernelStat(t, "SNATTCPReverseMissRSTs", status.Kernel.SNATTCPReverseMissRSTs, 7)
	assertKernelStat(t, "SNATTCPReverseMissACKs", status.Kernel.SNATTCPReverseMissACKs, 11)
	assertKernelStat(t, "SNATTCPReverseMissOther", status.Kernel.SNATTCPReverseMissOther, 13)
	assertKernelStat(t, "LocalhostConnectHits", status.Kernel.LocalhostConnectHits, 7)
	wantSNATMaps := SNATMapStats{
		FwdEntries:             10,
		FwdTCPEntries:          3,
		FwdUDPEntries:          5,
		FwdICMPEntries:         2,
		FwdActiveEntries:       4,
		FwdClosingEntries:      6,
		FwdOrigClosingEntries:  2,
		FwdReplyClosingEntries: 1,
		FwdFullClosingEntries:  3,
		RevEntries:             12,
		RevTCPEntries:          4,
		RevUDPEntries:          6,
		RevICMPEntries:         2,
		RevActiveEntries:       5,
		RevClosingEntries:      7,
		RevOrigClosingEntries:  2,
		RevReplyClosingEntries: 2,
		RevFullClosingEntries:  3,
		RevReverseEntries:      9,
		RevAliasEntries:        3,
		TranslatedPortsUsed:    7,
		UDPTranslatedPortsUsed: 5,
	}
	if status.SNATMaps != wantSNATMaps {
		t.Fatalf("unexpected snat map stats: %#v", status.SNATMaps)
	}
	if !status.Attachment.IngressTCAttached || !status.Attachment.EgressTCAttached || !status.Attachment.LocalhostLinksAttached {
		t.Fatalf("unexpected attachment readiness: %#v", status.Attachment)
	}
	if !status.Attachment.PinnedMapsReady || !status.Attachment.PinnedProgramsReady {
		t.Fatalf("expected pinned dataplane objects to be marked ready: %#v", status.Attachment)
	}
}

func assertKernelStat(t *testing.T, name string, got, want uint64) {
	t.Helper()
	if got != want {
		t.Fatalf("kernel stat %s: got %d, want %d", name, got, want)
	}
}

func TestWriteJSONFileCreatesParents(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "state.json")
	if err := writeJSONFile(target, map[string]string{"ok": "true"}); err != nil {
		t.Fatalf("write json file: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("stat written file: %v", err)
	}
}
