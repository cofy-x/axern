package natbench

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
)

func TestAggregateReportsUsesMedianPerPathMetric(t *testing.T) {
	reports := []Report{
		testReport("ebpf", 100, 2.0, 10, 200),
		testReport("ebpf", 300, 6.0, 30, 600),
		testReport("ebpf", 200, 4.0, 20, 400),
	}

	aggregated, err := AggregateReports(reports)
	if err != nil {
		t.Fatalf("AggregateReports() error = %v", err)
	}
	if got := aggregated.Paths[0].Summary.ThroughputRPS; got != 200 {
		t.Fatalf("expected median throughput 200, got %v", got)
	}
	if got := aggregated.Paths[0].Summary.Latency.P95Ms; got != 4.0 {
		t.Fatalf("expected median p95 4.0, got %v", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATMappingsProgrammed; got != 20 {
		t.Fatalf("expected median kernel delta 20, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATUDPSamePortHits; got != 400 {
		t.Fatalf("expected median udp same-port kernel delta 400, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATFullCloseReclaims; got != 40 {
		t.Fatalf("expected median full-close reclaim kernel delta 40, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATFullCloseMarks; got != 60 {
		t.Fatalf("expected median full-close mark kernel delta 60, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATTCPNonSynMissACKs; got != 100 {
		t.Fatalf("expected median tcp non-syn ACK miss kernel delta 100, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATTCPFullCloseDeletes; got != 140 {
		t.Fatalf("expected median tcp full-close delete kernel delta 140, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATTCPNonSynMissFwdLookups; got != 160 {
		t.Fatalf("expected median tcp non-syn fwd lookup miss kernel delta 160, got %d", got)
	}
	if got := aggregated.Paths[0].KernelDelta.SNATTCPReverseMisses; got != 220 {
		t.Fatalf("expected median tcp reverse miss kernel delta 220, got %d", got)
	}
	if got := aggregated.Paths[0].Profile.SNATUDPSamePortRatio; got != 0.4 {
		t.Fatalf("expected median udp same-port ratio 0.4, got %v", got)
	}
	if got := aggregated.Paths[0].SNATMapAfter.TranslatedPortsUsed; got != 30 {
		t.Fatalf("expected median translated port usage 30, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapAfter.UDPTranslatedPortsUsed; got != 23 {
		t.Fatalf("expected median udp translated port usage 23, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapAfter.FwdUDPEntries; got != 26 {
		t.Fatalf("expected median fwd udp entries 26, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapAfter.FwdFullClosingEntries; got != 30 {
		t.Fatalf("expected median fwd full-closing entries 30, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapDelta.FwdEntries; got != 2 {
		t.Fatalf("expected median fwd map delta 2, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapPostGC.TranslatedPortsUsed; got != 21 {
		t.Fatalf("expected median post-gc translated port usage 21, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapPostGC.UDPTranslatedPortsUsed; got != 21 {
		t.Fatalf("expected median post-gc udp translated port usage 21, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapGCReleased.FwdEntries; got != 19 {
		t.Fatalf("expected median released fwd entries 19, got %d", got)
	}
	if got := aggregated.Paths[0].SNATMapGCReleased.UDPTranslatedPortsUsed; got != 3 {
		t.Fatalf("expected median released udp translated port usage 3, got %d", got)
	}
	if got := aggregated.Paths[0].ClientAfter.Sockstat.TCPTimeWait; got != 30 {
		t.Fatalf("expected median client time-wait sockets 30, got %d", got)
	}
	if got := aggregated.Paths[0].ClientDelta.TCPTimeWait; got != 10 {
		t.Fatalf("expected median client time-wait delta 10, got %d", got)
	}
	if got := aggregated.Paths[0].ClientPeakDelta.TCPTimeWait; got != 20 {
		t.Fatalf("expected median client peak time-wait delta 20, got %d", got)
	}
	if got := aggregated.Paths[0].ClientSamples; got != 8 {
		t.Fatalf("expected median client sample count 8, got %d", got)
	}
	if aggregated.Startup == nil {
		t.Fatal("expected aggregated startup summary")
	}
	if got := aggregated.Startup.Classes["warm"].OKCount; got != 6 {
		t.Fatalf("expected aggregated warm startup count 6, got %d", got)
	}
	if got := aggregated.Startup.Phases["resource_allocate"].Classes["warm"].Count; got != 9 {
		t.Fatalf("expected aggregated resource_allocate warm count 9, got %d", got)
	}
	if aggregated.Startup.WaitGrace == nil {
		t.Fatal("expected aggregated waitGrace summary")
	}
	if got := aggregated.Startup.WaitGrace.RecoveredCount; got != 3 {
		t.Fatalf("expected aggregated waitGrace recovered count 3, got %d", got)
	}
	if got := aggregated.Startup.WaitGrace.UnavailableCount; got != 0 {
		t.Fatalf("expected aggregated waitGrace unavailable count 0, got %d", got)
	}
	if aggregated.Startup.Bundle == nil {
		t.Fatal("expected aggregated bundle summary")
	}
	if got := aggregated.Startup.Bundle.HitCount; got != 6 {
		t.Fatalf("expected aggregated bundle hit count 6, got %d", got)
	}
	if got := aggregated.Startup.Bundle.MissCount; got != 3 {
		t.Fatalf("expected aggregated bundle miss count 3, got %d", got)
	}
	if aggregated.Startup.Envelope == nil {
		t.Fatal("expected aggregated execution envelope summary")
	}
	if got := aggregated.Startup.Envelope.PreparedCount; got != 3 {
		t.Fatalf("expected aggregated execution envelope prepared count 3, got %d", got)
	}
	if got := aggregated.Startup.Envelope.HitCount; got != 3 {
		t.Fatalf("expected aggregated execution envelope hit count 3, got %d", got)
	}
}

func TestBuildCompareReportAggregatesBackends(t *testing.T) {
	iptablesRuns := []Report{
		testReport("iptables", 100, 2.0, 5, 100),
		testReport("iptables", 120, 2.2, 7, 120),
		testReport("iptables", 110, 2.1, 6, 110),
	}
	ebpfRuns := []Report{
		testReport("ebpf", 140, 1.8, 9, 140),
		testReport("ebpf", 150, 1.7, 10, 150),
		testReport("ebpf", 145, 1.75, 11, 145),
	}
	ebpfRuns[1].Paths[0].Summary.Successes = 995
	ebpfRuns[1].Paths[0].Summary.Failures = 5
	ebpfRuns[1].Paths[0].Summary.FirstError = "read timeout"

	report, err := BuildCompareReport(iptablesRuns, ebpfRuns)
	if err != nil {
		t.Fatalf("BuildCompareReport() error = %v", err)
	}
	if report.Aggregation.IptablesRuns != 3 || report.Aggregation.EBPFRuns != 3 {
		t.Fatalf("unexpected aggregation metadata: %#v", report.Aggregation)
	}
	if got := report.Comparison[0].Iptables.ThroughputRPS; got != 110 {
		t.Fatalf("expected median iptables throughput 110, got %v", got)
	}
	if got := report.Comparison[0].EBPF.ThroughputRPS; got != 145 {
		t.Fatalf("expected median ebpf throughput 145, got %v", got)
	}
	if report.Comparison[0].Delta.ThroughputPct == nil {
		t.Fatalf("expected throughput delta")
	}
	if got := report.Comparison[0].EBPF.TotalRequests; got != 3000 {
		t.Fatalf("expected ebpf total requests 3000, got %d", got)
	}
	if got := report.Comparison[0].EBPF.TotalFailures; got != 5 {
		t.Fatalf("expected ebpf total failures 5, got %d", got)
	}
	if got := report.Comparison[0].EBPF.RunsWithFailures; got != 1 {
		t.Fatalf("expected one ebpf run with failures, got %d", got)
	}
	if got := report.Comparison[0].EBPF.MaxFailures; got != 5 {
		t.Fatalf("expected ebpf max failures 5, got %d", got)
	}
	if got := report.Comparison[0].EBPF.FirstError; got != "read timeout" {
		t.Fatalf("expected ebpf first error to be preserved, got %q", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapAfter.TranslatedPortsUsed; got != 20 {
		t.Fatalf("expected ebpf translated port usage in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapAfter.UDPTranslatedPortsUsed; got != 13 {
		t.Fatalf("expected ebpf udp translated port usage in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapPostGC.TranslatedPortsUsed; got != 11 {
		t.Fatalf("expected ebpf post-gc translated port usage in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapPostGC.UDPTranslatedPortsUsed; got != 11 {
		t.Fatalf("expected ebpf post-gc udp translated port usage in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapGCReleased.FwdEntries; got != 9 {
		t.Fatalf("expected ebpf released fwd entries in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.SNATMapGCReleased.UDPTranslatedPortsUsed; got != 3 {
		t.Fatalf("expected ebpf released udp translated port usage in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.ClientDelta.TCPTimeWait; got != 10 {
		t.Fatalf("expected ebpf client resource delta in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.ClientPeakDelta.TCPTimeWait; got != 20 {
		t.Fatalf("expected ebpf client peak resource delta in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.KernelDelta.SNATTCPNonSynMisses; got != 80 {
		t.Fatalf("expected ebpf tcp non-syn misses in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.KernelDelta.SNATTCPNonSynMissACKs; got != 50 {
		t.Fatalf("expected ebpf tcp non-syn ACK misses in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.KernelDelta.SNATTCPFullCloseDeletesFwd; got != 30 {
		t.Fatalf("expected ebpf tcp fwd full-close deletes in comparison, got %d", got)
	}
	if got := report.Comparison[0].EBPF.KernelDelta.SNATTCPReverseMissSynACKs; got != 70 {
		t.Fatalf("expected ebpf tcp reverse SYN-ACK misses in comparison, got %d", got)
	}
	if got := report.Comparison[0].Iptables.KernelDelta.SNATTCPNonSynMissRSTs; got != 12 {
		t.Fatalf("expected iptables tcp non-syn RST misses in comparison, got %d", got)
	}
	if got := report.EBPF.Paths[0].Summary.FirstError; got != "" {
		t.Fatalf("expected median-success aggregate summary to omit first error, got %q", got)
	}
}

func testReport(backend string, throughput, p95 float64, mappings uint64, cpu float64) Report {
	observed := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	return Report{
		Runtime:    "runsc",
		NATBackend: backend,
		Startup: &StartupSummary{
			Runtime:    "runsc",
			RootfsType: "local",
			Classes: map[string]StartupClassSummary{
				"warm": {
					OKCount:                2,
					AverageDurationSeconds: p95 / 1000,
				},
			},
			Phases: map[string]StartupPhaseSummary{
				"resource_allocate": {
					Classes: map[string]StartupPhaseClassSummary{
						"warm": {
							Count:                  3,
							AverageDurationSeconds: p95 / 2000,
						},
					},
				},
			},
			WaitGrace: &RuntimeWaitGraceSummary{
				RecoveredCount:   1,
				UnavailableCount: 0,
			},
			Bundle: &BundleTemplateSummary{
				HitCount:                      2,
				MissCount:                     1,
				AverageMaterializeDurationSec: p95 / 5000,
			},
			Envelope: &ExecutionEnvelopeSummary{
				PreparedCount:              1,
				HitCount:                   1,
				AveragePrepareDurationSec:  p95 / 4000,
				AverageActivateDurationSec: p95 / 8000,
			},
		},
		StartedAt:   observed,
		CompletedAt: observed.Add(2 * time.Second),
		Paths: []PathBenchmark{
			{
				Name:     "egress_udp",
				Datapath: backend,
				Mode:     "steady",
				Summary: WorkloadSummary{
					Requests:        1000,
					Concurrency:     16,
					Successes:       1000,
					Failures:        0,
					DurationSeconds: 1,
					ThroughputRPS:   throughput,
					Latency: LatencySummary{
						P50Ms: p95 / 2,
						P95Ms: p95,
						P99Ms: p95 * 1.2,
					},
				},
				HostCPUPercent: cpu,
				ClientBefore: ClientResourceSnapshot{
					OpenFileLimit:      1048576,
					EphemeralPortFirst: 32768,
					EphemeralPortLast:  60999,
					EphemeralPortCount: 28232,
					Sockstat: SocketStats{
						TCPTimeWait: mappings,
					},
				},
				ClientAfter: ClientResourceSnapshot{
					OpenFileLimit:      1048576,
					EphemeralPortFirst: 32768,
					EphemeralPortLast:  60999,
					EphemeralPortCount: 28232,
					Sockstat: SocketStats{
						TCPTimeWait: mappings + 10,
					},
				},
				ClientPeak: ClientResourceSnapshot{
					OpenFileLimit:      1048576,
					EphemeralPortFirst: 32768,
					EphemeralPortLast:  60999,
					EphemeralPortCount: 28232,
					Sockstat: SocketStats{
						TCPTimeWait: mappings + 20,
					},
				},
				ClientDelta: ClientResourceDelta{
					TCPTimeWait: 10,
				},
				ClientPeakDelta: ClientResourceDelta{
					TCPTimeWait: 20,
				},
				ClientSamples: 8,
				KernelDelta: bpfnet.KernelStats{
					SNATMappingsProgrammed:             mappings,
					SNATUDPSamePortHits:                uint64(throughput * 2),
					SNATTCPNonSynMisses:                mappings * 8,
					SNATTCPNonSynMissFINs:              mappings,
					SNATTCPNonSynMissRSTs:              mappings * 2,
					SNATTCPNonSynMissACKs:              mappings * 5,
					SNATTCPNonSynMissOther:             0,
					SNATFullCloseReclaims:              mappings * 2,
					SNATFullCloseMarks:                 mappings * 3,
					SNATTCPFullCloseDeletes:            mappings * 7,
					SNATTCPFullCloseDeletesFwd:         mappings * 3,
					SNATTCPFullCloseDeletesRev:         mappings * 4,
					SNATTCPNonSynMissFwdLookups:        mappings * 8,
					SNATTCPNonSynMissFwdHostMismatches: mappings,
					SNATTCPReverseMisses:               mappings * 11,
					SNATTCPReverseMissSynACKs:          mappings * 7,
					SNATTCPReverseMissFINs:             mappings,
					SNATTCPReverseMissRSTs:             mappings * 2,
					SNATTCPReverseMissACKs:             mappings,
					SNATTCPReverseMissOther:            0,
				},
				SNATMapBefore: bpfnet.SNATMapStats{
					FwdEntries:             mappings,
					FwdTCPEntries:          mappings / 2,
					FwdUDPEntries:          mappings,
					FwdClosingEntries:      mappings,
					FwdFullClosingEntries:  mappings,
					RevEntries:             mappings * 2,
					RevTCPEntries:          mappings,
					RevUDPEntries:          mappings * 2,
					RevClosingEntries:      mappings * 2,
					RevFullClosingEntries:  mappings * 2,
					RevReverseEntries:      mappings,
					TranslatedPortsUsed:    mappings,
					UDPTranslatedPortsUsed: mappings,
				},
				SNATMapAfter: bpfnet.SNATMapStats{
					FwdEntries:             mappings + 2,
					FwdTCPEntries:          mappings/2 + 1,
					FwdUDPEntries:          mappings + 6,
					FwdClosingEntries:      mappings + 2,
					FwdFullClosingEntries:  mappings + 10,
					RevEntries:             mappings*2 + 4,
					RevTCPEntries:          mappings + 2,
					RevUDPEntries:          mappings*2 + 6,
					RevClosingEntries:      mappings*2 + 4,
					RevFullClosingEntries:  mappings*2 + 10,
					RevReverseEntries:      mappings + 2,
					TranslatedPortsUsed:    mappings + 10,
					UDPTranslatedPortsUsed: mappings + 3,
				},
				SNATMapDelta: bpfnet.SNATMapStats{
					FwdEntries:             2,
					FwdTCPEntries:          1,
					FwdUDPEntries:          6,
					FwdClosingEntries:      2,
					FwdFullClosingEntries:  10,
					RevEntries:             4,
					RevTCPEntries:          2,
					RevUDPEntries:          6,
					RevClosingEntries:      4,
					RevFullClosingEntries:  10,
					RevReverseEntries:      2,
					TranslatedPortsUsed:    10,
					UDPTranslatedPortsUsed: 3,
				},
				SNATMapPostGC: bpfnet.SNATMapStats{
					FwdEntries:             1,
					FwdTCPEntries:          0,
					FwdUDPEntries:          1,
					FwdClosingEntries:      1,
					FwdFullClosingEntries:  1,
					RevEntries:             2,
					RevTCPEntries:          0,
					RevUDPEntries:          2,
					RevClosingEntries:      2,
					RevFullClosingEntries:  2,
					RevReverseEntries:      1,
					TranslatedPortsUsed:    mappings + 1,
					UDPTranslatedPortsUsed: mappings + 1,
				},
				SNATMapGCReleased: bpfnet.SNATMapStats{
					FwdEntries:             mappings - 1,
					FwdTCPEntries:          mappings / 2,
					FwdUDPEntries:          mappings - 1,
					FwdClosingEntries:      mappings - 1,
					FwdFullClosingEntries:  mappings - 1,
					RevEntries:             mappings*2 + 2,
					RevTCPEntries:          mappings,
					RevUDPEntries:          mappings*2 + 4,
					RevClosingEntries:      mappings*2 + 2,
					RevFullClosingEntries:  mappings*2 + 2,
					RevReverseEntries:      mappings + 1,
					TranslatedPortsUsed:    9,
					UDPTranslatedPortsUsed: 3,
				},
				Profile: NATProfile{
					SNATMappingsPerSuccess: float64(mappings) / 1000,
					SNATUDPSamePortRatio:   throughput / 500,
				},
				Attachment: bpfnet.AttachmentReadiness{
					UplinkDevices:     []string{"eth0"},
					LocalAddresses:    []string{"192.168.215.2"},
					IngressTCAttached: true,
					EgressTCAttached:  true,
					PinnedMapsReady:   true,
				},
				ObservedAt: observed,
			},
		},
	}
}
