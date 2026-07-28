package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func TestBuildEgressPathBenchmarkEBPFIncludesPerTransportDelta(t *testing.T) {
	spec := benchmarkTransportSpec{
		Name:      "egress_udp",
		Transport: "udp",
	}
	var cpuBefore natbench.CPUSnapshot
	var cpuAfter natbench.CPUSnapshot
	clientBefore := natbench.ClientResourceSnapshot{
		Sockstat: natbench.SocketStats{
			SocketsUsed: 10,
			TCPTimeWait: 20,
		},
	}
	clientAfter := natbench.ClientResourceSnapshot{
		Sockstat: natbench.SocketStats{
			SocketsUsed: 14,
			TCPTimeWait: 35,
		},
	}
	clientPeak := natbench.ClientResourceSnapshot{
		Sockstat: natbench.SocketStats{
			SocketsUsed: 22,
			TCPTimeWait: 44,
		},
	}
	statusBefore := bpfnet.Status{
		Kernel: bpfnet.KernelStats{
			SNATHits:               10,
			SNATRevHits:            8,
			SNATFwdHits:            5,
			SNATMappingsProgrammed: 3,
			SNATAllocCollisions:    1,
		},
		SNATMaps: bpfnet.SNATMapStats{
			FwdEntries:             10,
			FwdUDPEntries:          10,
			RevEntries:             12,
			RevUDPEntries:          12,
			TranslatedPortsUsed:    7,
			UDPTranslatedPortsUsed: 7,
		},
	}
	statusAfter := bpfnet.Status{
		State: bpfnet.DataplaneState{
			Mode: "ingress-tcp-udp-dnat+egress-snat+localhost-tcp-dnat+iptables-safety-fallback",
		},
		Kernel: bpfnet.KernelStats{
			SNATHits:               25,
			SNATRevHits:            20,
			SNATFwdHits:            14,
			SNATMappingsProgrammed: 6,
			SNATAllocCollisions:    1,
		},
		SNATMaps: bpfnet.SNATMapStats{
			FwdEntries:             15,
			FwdUDPEntries:          15,
			RevEntries:             10,
			RevUDPEntries:          10,
			TranslatedPortsUsed:    9,
			UDPTranslatedPortsUsed: 9,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}
	statusPostGC := statusAfter
	statusPostGC.SNATMaps = bpfnet.SNATMapStats{
		FwdEntries:             3,
		FwdUDPEntries:          3,
		RevEntries:             4,
		RevUDPEntries:          4,
		TranslatedPortsUsed:    2,
		UDPTranslatedPortsUsed: 2,
	}
	summary := natbench.WorkloadSummary{
		Requests:      100,
		Successes:     100,
		ThroughputRPS: 1234,
	}

	path := buildEgressPathBenchmark(config.NatBackendEBPF, spec, egressPathSnapshots{
		CPUBefore:     cpuBefore,
		CPUAfter:      cpuAfter,
		ClientBefore:  clientBefore,
		ClientAfter:   clientAfter,
		ClientPeak:    clientPeak,
		ClientSamples: 12,
		StatusBefore:  statusBefore,
		StatusAfter:   statusAfter,
		StatusPostGC:  statusPostGC,
	}, summary)

	if path.Name != "egress_udp" {
		t.Fatalf("unexpected path name: %#v", path)
	}
	if path.Mode != statusAfter.State.Mode {
		t.Fatalf("expected mode %q, got %#v", statusAfter.State.Mode, path)
	}
	if path.KernelDelta.SNATHits != 15 || path.KernelDelta.SNATFwdHits != 9 || path.KernelDelta.SNATMappingsProgrammed != 3 {
		t.Fatalf("unexpected kernel delta: %#v", path.KernelDelta)
	}
	if path.SNATMapBefore.FwdEntries != 10 || path.SNATMapAfter.FwdEntries != 15 || path.SNATMapDelta.FwdEntries != 5 {
		t.Fatalf("unexpected snat fwd map stats: before=%#v after=%#v delta=%#v", path.SNATMapBefore, path.SNATMapAfter, path.SNATMapDelta)
	}
	if path.SNATMapDelta.RevEntries != 0 || path.SNATMapDelta.TranslatedPortsUsed != 2 {
		t.Fatalf("unexpected saturating snat map delta: %#v", path.SNATMapDelta)
	}
	if path.SNATMapDelta.FwdUDPEntries != 5 || path.SNATMapDelta.RevUDPEntries != 0 || path.SNATMapDelta.UDPTranslatedPortsUsed != 2 {
		t.Fatalf("unexpected udp snat map delta: %#v", path.SNATMapDelta)
	}
	if path.SNATMapPostGC.FwdEntries != 3 || path.SNATMapGCReleased.FwdEntries != 12 {
		t.Fatalf("unexpected post-gc snat map stats: postGC=%#v released=%#v", path.SNATMapPostGC, path.SNATMapGCReleased)
	}
	if path.SNATMapPostGC.FwdUDPEntries != 3 || path.SNATMapGCReleased.FwdUDPEntries != 12 || path.SNATMapGCReleased.UDPTranslatedPortsUsed != 7 {
		t.Fatalf("unexpected post-gc udp snat map stats: postGC=%#v released=%#v", path.SNATMapPostGC, path.SNATMapGCReleased)
	}
	if path.Profile.SNATMappingsPerSuccess != 0.03 {
		t.Fatalf("expected derived snat profile, got %#v", path.Profile)
	}
	if !path.Attachment.IngressTCAttached || !path.Attachment.EgressTCAttached {
		t.Fatalf("expected attachment readiness to be copied: %#v", path.Attachment)
	}
	if path.HostCPUPercent != 0 {
		t.Fatalf("expected zero host cpu percent with zero snapshots: %#v", path)
	}
	if path.ClientDelta.SocketsUsed != 4 || path.ClientDelta.TCPTimeWait != 15 {
		t.Fatalf("expected client resource delta, got %#v", path.ClientDelta)
	}
	if path.ClientPeakDelta.SocketsUsed != 12 || path.ClientPeakDelta.TCPTimeWait != 24 || path.ClientSamples != 12 {
		t.Fatalf("expected client peak resource delta, got delta=%#v samples=%d", path.ClientPeakDelta, path.ClientSamples)
	}
	if len(path.Notes) < 2 {
		t.Fatalf("expected per-transport notes: %#v", path.Notes)
	}
}

func TestReadBenchmarkSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")
	if err := os.WriteFile(path, []byte(`{"requests":10,"successes":9,"failures":1,"durationSeconds":1.5,"throughputRps":6}`), 0644); err != nil {
		t.Fatalf("write summary fixture: %v", err)
	}

	summary, err := readBenchmarkSummary(path)
	if err != nil {
		t.Fatalf("readBenchmarkSummary returned error: %v", err)
	}
	if summary.Requests != 10 || summary.Successes != 9 || summary.Failures != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestAppendTransportSuffix(t *testing.T) {
	if got := appendTransportSuffix("/tmp/benchmark-egress.json", "udp"); got != "/tmp/benchmark-egress-udp.json" {
		t.Fatalf("unexpected suffixed path: %s", got)
	}
	if got := appendTransportSuffix("/tmp/benchmark-egress", "tcp"); got != "/tmp/benchmark-egress-tcp" {
		t.Fatalf("unexpected suffix without extension: %s", got)
	}
}

func TestSanitizeTransport(t *testing.T) {
	if got := sanitizeTransport("udp-connected"); got != "udp_connected" {
		t.Fatalf("unexpected sanitized transport: %s", got)
	}
	if got := sanitizeTransport(" TCP "); got != "tcp" {
		t.Fatalf("unexpected sanitized transport casing: %s", got)
	}
}
