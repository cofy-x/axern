package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestBuildProbeCommandIncludesTransportAndConcurrency(t *testing.T) {
	cmd := buildProbeCommand("benchmark", "udp-connected", "udp-connected", "1.1.1.1:1", "2.2.2.2:2", "3.3.3.3", "4.4.4.4", 2*time.Second, 100, 8, 32, time.Second)
	joined := ""
	for _, part := range cmd {
		joined += part + " "
	}
	if !containsAll(joined, "-mode benchmark", "-transport udp-connected", "-benchmark-transports udp-connected", "-requests 100", "-concurrency 8", "-warmup-requests 32", "-phase-delay 1s") {
		t.Fatalf("unexpected command arguments: %v", cmd)
	}
}

func TestSelectBenchmarkTransportSpecs(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("tcp-reuse,udp,udp-connected")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	if len(specs) != 3 || specs[0].Name != "egress_tcp_reuse" || specs[0].Transport != "tcp-reuse" || specs[1].Transport != "udp" || specs[2].Transport != "udp-connected" {
		t.Fatalf("unexpected transport specs: %#v", specs)
	}
}

func TestSelectBenchmarkTransportSpecsDefaultsToSingleShortTCP(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "egress_tcp_short" || specs[0].Transport != "tcp-short" {
		t.Fatalf("unexpected default transport specs: %#v", specs)
	}
}

func TestValidateBenchmarkPostGCSnapshotRejectsMultiTransportEBPF(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("tcp-short,udp")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	err = validateBenchmarkPostGCSnapshot(config.NatBackendEBPF, time.Second, specs)
	if err == nil {
		t.Fatalf("expected multi-transport post-GC eBPF benchmark to fail validation")
	}
	if !strings.Contains(err.Error(), "single eBPF benchmark transport") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateBenchmarkPostGCSnapshotAllowsMultiTransportWithoutPostGC(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("tcp-short,udp")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	if err := validateBenchmarkPostGCSnapshot(config.NatBackendEBPF, 0, specs); err != nil {
		t.Fatalf("expected disabled post-GC wait to allow multi transport: %v", err)
	}
	if err := validateBenchmarkPostGCSnapshot(config.NatBackendIptables, time.Second, specs); err != nil {
		t.Fatalf("expected iptables benchmark to allow multi transport: %v", err)
	}
}

func TestApplyBenchmarkClientCountRenamesShortTCPPath(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("tcp-short")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	specs, err = applyBenchmarkClientCount(specs, 4)
	if err != nil {
		t.Fatalf("applyBenchmarkClientCount returned error: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "egress_tcp_short_multi_client" || specs[0].Transport != "tcp-short" {
		t.Fatalf("unexpected multi-client specs: %#v", specs)
	}
	if len(specs[0].Notes) < 2 {
		t.Fatalf("expected multi-client notes: %#v", specs[0].Notes)
	}
}

func TestApplyBenchmarkClientCountRejectsNonShortTCP(t *testing.T) {
	specs, err := selectBenchmarkTransportSpecs("tcp-reuse")
	if err != nil {
		t.Fatalf("selectBenchmarkTransportSpecs returned error: %v", err)
	}
	if _, err := applyBenchmarkClientCount(specs, 2); err == nil {
		t.Fatal("expected multi-client non-short tcp benchmark to fail validation")
	}
}

func TestSplitEvenly(t *testing.T) {
	got := splitEvenly(10, 4)
	want := []int{3, 3, 2, 2}
	if len(got) != len(want) {
		t.Fatalf("unexpected split length: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected split: got %#v want %#v", got, want)
		}
	}
}

func TestBenchmarkSuiteWaitTimeoutScalesWithLargeSuites(t *testing.T) {
	timeout := benchmarkSuiteWaitTimeout(10*time.Second, 50_000, 16, 3)
	if timeout <= waitTimeout {
		t.Fatalf("expected large benchmark suite timeout to exceed default wait timeout, got %s", timeout)
	}
	if timeout > 30*time.Minute {
		t.Fatalf("expected benchmark suite timeout to stay capped, got %s", timeout)
	}
}

func TestBenchmarkSuiteWaitTimeoutKeepsSmallSuitesShort(t *testing.T) {
	timeout := benchmarkSuiteWaitTimeout(10*time.Second, 0, 16, 3)
	if timeout != waitTimeout {
		t.Fatalf("expected small suite timeout %s, got %s", waitTimeout, timeout)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
