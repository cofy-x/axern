package natbench

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
)

func TestSummarize(t *testing.T) {
	summary := Summarize(
		10,
		4,
		8,
		2,
		2*time.Second,
		[]time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			30 * time.Millisecond,
			40 * time.Millisecond,
			50 * time.Millisecond,
		},
	)

	if summary.ThroughputRPS != 4 {
		t.Fatalf("unexpected throughput: %#v", summary)
	}
	if summary.Latency.P50Ms <= 0 || summary.Latency.P95Ms <= 0 || summary.Latency.P99Ms <= 0 {
		t.Fatalf("expected percentiles to be populated: %#v", summary)
	}
}

func TestCombineConcurrentWorkloadSummaries(t *testing.T) {
	summary := CombineConcurrentWorkloadSummaries([]WorkloadSummary{
		{
			Requests:        10,
			Concurrency:     2,
			Successes:       10,
			DurationSeconds: 2,
			ThroughputRPS:   5,
			Latency: LatencySummary{
				P50Ms: 1,
				P95Ms: 5,
				P99Ms: 9,
			},
		},
		{
			Requests:        10,
			Concurrency:     2,
			Successes:       9,
			Failures:        1,
			FirstError:      "boom",
			DurationSeconds: 3,
			ThroughputRPS:   3,
			Latency: LatencySummary{
				P50Ms: 2,
				P95Ms: 7,
				P99Ms: 8,
			},
		},
	}, 4)

	if summary.Requests != 20 || summary.Successes != 19 || summary.Failures != 1 || summary.Concurrency != 4 {
		t.Fatalf("unexpected combined counts: %#v", summary)
	}
	if summary.DurationSeconds != 3 || summary.ThroughputRPS != float64(19)/3 {
		t.Fatalf("unexpected combined duration/throughput: %#v", summary)
	}
	if summary.FirstError != "boom" || summary.Latency.P50Ms != 2 || summary.Latency.P95Ms != 7 || summary.Latency.P99Ms != 9 {
		t.Fatalf("unexpected combined error/latency: %#v", summary)
	}
}

func TestCPUUsagePercent(t *testing.T) {
	before := CPUSnapshot{total: 100, idle: 40}
	after := CPUSnapshot{total: 200, idle: 90}

	usage := CPUUsagePercent(before, after)
	if usage <= 0 || usage >= 100 {
		t.Fatalf("expected bounded cpu usage, got %.2f", usage)
	}
}

func TestRunWorkloadWithWorkerSetupOncePerWorker(t *testing.T) {
	var setups atomic.Int32
	var probes atomic.Int32

	summary := RunWorkloadWithWorker(8, 4, func() (func() error, func(), error) {
		setups.Add(1)
		return func() error {
			probes.Add(1)
			return nil
		}, func() {}, nil
	})

	if summary.Successes != 8 || summary.Failures != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if setups.Load() != 4 {
		t.Fatalf("expected one setup per worker, got %d", setups.Load())
	}
	if probes.Load() != 8 {
		t.Fatalf("expected one probe per request, got %d", probes.Load())
	}
}

func TestRunWorkloadWithWorkerSetupError(t *testing.T) {
	summary := RunWorkloadWithWorker(4, 2, func() (func() error, func(), error) {
		return nil, func() {}, errors.New("boom")
	})

	if summary.Successes != 0 || summary.Failures != 4 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.FirstError != "boom" {
		t.Fatalf("unexpected first error: %#v", summary)
	}
}

func TestRunWorkloadWarmupNotCountedInSummary(t *testing.T) {
	var probes atomic.Int32

	summary := RunWorkloadWarmup(6, 3, 4, func() error {
		probes.Add(1)
		return nil
	})

	if summary.Successes != 6 || summary.Failures != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if probes.Load() != 10 {
		t.Fatalf("expected warmup + measured probes, got %d", probes.Load())
	}
}

func TestRunWorkloadWithWorkerWarmupUsesSameWorkerSetup(t *testing.T) {
	var setups atomic.Int32
	var probes atomic.Int32

	summary := RunWorkloadWithWorkerWarmup(8, 4, 4, func() (func() error, func(), error) {
		setups.Add(1)
		return func() error {
			probes.Add(1)
			return nil
		}, func() {}, nil
	})

	if summary.Successes != 8 || summary.Failures != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if setups.Load() != 4 {
		t.Fatalf("expected one setup per worker, got %d", setups.Load())
	}
	if probes.Load() != 12 {
		t.Fatalf("expected warmup + measured probes, got %d", probes.Load())
	}
}

func TestBuildSNATProfile(t *testing.T) {
	profile := BuildSNATProfile(
		WorkloadSummary{Successes: 100},
		bpfnet.KernelStats{
			SNATHits:               120,
			SNATRevHits:            110,
			SNATFwdHits:            30,
			SNATUDPSamePortHits:    90,
			SNATUDPPortRewriteHits: 30,
			SNATUDPChecksumHits:    60,
			SNATMappingsProgrammed: 75,
			SNATFallbackHits:       30,
			SNATAllocExhausted:     25,
			SNATTCPNonSynMisses:    15,
		},
	)

	if profile.SNATMappingsPerSuccess != 0.75 {
		t.Fatalf("unexpected mappings per success: %#v", profile)
	}
	if profile.SNATForwardReuseRatio != 0.25 {
		t.Fatalf("unexpected forward reuse ratio: %#v", profile)
	}
	if profile.SNATReverseHitRatio != (110.0 / 120.0) {
		t.Fatalf("unexpected reverse hit ratio: %#v", profile)
	}
	if profile.SNATFallbackRatio != 0.2 {
		t.Fatalf("unexpected fallback ratio: %#v", profile)
	}
	if profile.SNATAllocExhaustedRatio != 0.25 {
		t.Fatalf("unexpected allocator exhaustion ratio: %#v", profile)
	}
	if profile.SNATTCPNonSynMissRatio != 0.1 {
		t.Fatalf("unexpected tcp non-syn miss ratio: %#v", profile)
	}
	if profile.SNATUDPSamePortRatio != 0.75 {
		t.Fatalf("unexpected udp same-port ratio: %#v", profile)
	}
	if profile.SNATUDPPortRewriteRatio != 0.25 {
		t.Fatalf("unexpected udp rewrite ratio: %#v", profile)
	}
	if profile.SNATUDPChecksumRatio != 0.5 {
		t.Fatalf("unexpected udp checksum ratio: %#v", profile)
	}
}

func TestSNATMapDeltaSaturatesAtZero(t *testing.T) {
	before := bpfnet.SNATMapStats{
		FwdEntries:             10,
		FwdTCPEntries:          6,
		FwdUDPEntries:          3,
		FwdICMPEntries:         1,
		FwdActiveEntries:       7,
		FwdClosingEntries:      3,
		FwdOrigClosingEntries:  1,
		FwdReplyClosingEntries: 1,
		FwdFullClosingEntries:  1,
		RevEntries:             20,
		RevTCPEntries:          4,
		RevUDPEntries:          12,
		RevICMPEntries:         4,
		RevActiveEntries:       11,
		RevClosingEntries:      9,
		RevOrigClosingEntries:  3,
		RevReplyClosingEntries: 3,
		RevFullClosingEntries:  3,
		RevReverseEntries:      12,
		RevAliasEntries:        8,
		TranslatedPortsUsed:    7,
		UDPTranslatedPortsUsed: 5,
	}
	after := bpfnet.SNATMapStats{
		FwdEntries:             4,
		FwdTCPEntries:          2,
		FwdUDPEntries:          5,
		FwdICMPEntries:         1,
		FwdActiveEntries:       8,
		FwdClosingEntries:      1,
		FwdOrigClosingEntries:  0,
		FwdReplyClosingEntries: 2,
		FwdFullClosingEntries:  1,
		RevEntries:             25,
		RevTCPEntries:          5,
		RevUDPEntries:          10,
		RevICMPEntries:         7,
		RevActiveEntries:       10,
		RevClosingEntries:      12,
		RevOrigClosingEntries:  4,
		RevReplyClosingEntries: 2,
		RevFullClosingEntries:  6,
		RevReverseEntries:      15,
		RevAliasEntries:        6,
		TranslatedPortsUsed:    9,
		UDPTranslatedPortsUsed: 8,
	}

	delta := SNATMapDelta(before, after)
	want := bpfnet.SNATMapStats{
		FwdUDPEntries:          2,
		FwdActiveEntries:       1,
		FwdReplyClosingEntries: 1,
		RevEntries:             5,
		RevTCPEntries:          1,
		RevICMPEntries:         3,
		RevClosingEntries:      3,
		RevOrigClosingEntries:  1,
		RevFullClosingEntries:  3,
		RevReverseEntries:      3,
		TranslatedPortsUsed:    2,
		UDPTranslatedPortsUsed: 3,
	}
	if delta != want {
		t.Fatalf("unexpected snat map delta: %#v", delta)
	}
}

func TestSNATMapReleasedSaturatesAtZero(t *testing.T) {
	beforeGC := bpfnet.SNATMapStats{
		FwdEntries:             10,
		FwdTCPEntries:          6,
		FwdUDPEntries:          3,
		FwdICMPEntries:         1,
		FwdActiveEntries:       7,
		FwdClosingEntries:      3,
		FwdOrigClosingEntries:  1,
		FwdReplyClosingEntries: 1,
		FwdFullClosingEntries:  1,
		RevEntries:             20,
		RevTCPEntries:          4,
		RevUDPEntries:          12,
		RevICMPEntries:         4,
		RevActiveEntries:       11,
		RevClosingEntries:      9,
		RevOrigClosingEntries:  3,
		RevReplyClosingEntries: 3,
		RevFullClosingEntries:  3,
		RevReverseEntries:      12,
		RevAliasEntries:        8,
		TranslatedPortsUsed:    7,
		UDPTranslatedPortsUsed: 8,
	}
	afterGC := bpfnet.SNATMapStats{
		FwdEntries:             4,
		FwdTCPEntries:          2,
		FwdUDPEntries:          5,
		FwdICMPEntries:         1,
		FwdActiveEntries:       8,
		FwdClosingEntries:      1,
		FwdOrigClosingEntries:  0,
		FwdReplyClosingEntries: 2,
		FwdFullClosingEntries:  1,
		RevEntries:             25,
		RevTCPEntries:          5,
		RevUDPEntries:          10,
		RevICMPEntries:         7,
		RevActiveEntries:       10,
		RevClosingEntries:      12,
		RevOrigClosingEntries:  4,
		RevReplyClosingEntries: 2,
		RevFullClosingEntries:  6,
		RevReverseEntries:      15,
		RevAliasEntries:        6,
		TranslatedPortsUsed:    9,
		UDPTranslatedPortsUsed: 4,
	}

	released := SNATMapReleased(beforeGC, afterGC)
	want := bpfnet.SNATMapStats{
		FwdEntries:             6,
		FwdTCPEntries:          4,
		FwdClosingEntries:      2,
		FwdOrigClosingEntries:  1,
		RevUDPEntries:          2,
		RevActiveEntries:       1,
		RevReplyClosingEntries: 1,
		RevAliasEntries:        2,
		UDPTranslatedPortsUsed: 4,
	}
	if released != want {
		t.Fatalf("unexpected snat map released stats: %#v", released)
	}
}

func TestMergeSNATMapStatsPeakKeepsCoherentSnapshot(t *testing.T) {
	current := bpfnet.SNATMapStats{
		FwdEntries:          100,
		RevEntries:          100,
		TranslatedPortsUsed: 80,
	}
	candidate := bpfnet.SNATMapStats{
		FwdEntries:            90,
		FwdFullClosingEntries: 90,
		RevEntries:            95,
		RevFullClosingEntries: 95,
		TranslatedPortsUsed:   81,
	}

	peak := MergeSNATMapStatsPeak(current, candidate)
	if peak != candidate {
		t.Fatalf("expected translated-port peak snapshot to win: %#v", peak)
	}

	tieWithMoreEntries := bpfnet.SNATMapStats{
		FwdEntries:          120,
		RevEntries:          120,
		TranslatedPortsUsed: 81,
	}
	peak = MergeSNATMapStatsPeak(candidate, tieWithMoreEntries)
	if peak != tieWithMoreEntries {
		t.Fatalf("expected entry-count tie-breaker to preserve one coherent snapshot: %#v", peak)
	}
}
