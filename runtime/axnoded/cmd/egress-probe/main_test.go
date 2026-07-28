package main

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func TestBuildBenchmarkSuiteHonorsTransportSubset(t *testing.T) {
	t.Parallel()

	seen := make([]string, 0, 2)
	suite, err := buildBenchmarkSuite([]string{"tcp-reuse", "udp", "udp-connected"}, func(transport string) (natbench.WorkloadSummary, error) {
		seen = append(seen, transport)
		return natbench.WorkloadSummary{
			Requests:  map[string]int{"tcp-reuse": 5, "udp": 11, "udp-connected": 17}[transport],
			Successes: map[string]int{"tcp-reuse": 3, "udp": 7, "udp-connected": 13}[transport],
		}, nil
	})
	if err != nil {
		t.Fatalf("buildBenchmarkSuite() error = %v", err)
	}

	if got, want := seen, []string{"tcp-reuse", "udp", "udp-connected"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("buildBenchmarkSuite() ran transports %v, want %v", got, want)
	}
	if suite.TCPShort.Requests != 0 || suite.TCPShort.Successes != 0 {
		t.Fatalf("unexpected TCP short summary for omitted transport: %+v", suite.TCPShort)
	}
	if suite.TCPReuse.Requests != 5 || suite.TCPReuse.Successes != 3 {
		t.Fatalf("unexpected TCP reuse summary: %+v", suite.TCPReuse)
	}
	if suite.UDP.Requests != 11 || suite.UDP.Successes != 7 {
		t.Fatalf("unexpected UDP summary: %+v", suite.UDP)
	}
	if suite.UDPConnected.Requests != 17 || suite.UDPConnected.Successes != 13 {
		t.Fatalf("unexpected UDP connected summary: %+v", suite.UDPConnected)
	}
}

func TestBuildBenchmarkSuiteRejectsUnsupportedTransport(t *testing.T) {
	t.Parallel()

	_, err := buildBenchmarkSuite([]string{"bogus"}, func(string) (natbench.WorkloadSummary, error) {
		return natbench.WorkloadSummary{}, nil
	})
	if err == nil {
		t.Fatal("buildBenchmarkSuite() error = nil, want unsupported transport error")
	}
}

func TestSelectedBenchmarkTransportsDefaultsToShortTCP(t *testing.T) {
	t.Parallel()

	transports, err := selectedBenchmarkTransports("")
	if err != nil {
		t.Fatalf("selectedBenchmarkTransports() error = %v", err)
	}
	want := []string{"tcp-short"}
	if len(transports) != len(want) {
		t.Fatalf("transports = %v, want %v", transports, want)
	}
	for i := range want {
		if transports[i] != want[i] {
			t.Fatalf("transports = %v, want %v", transports, want)
		}
	}
}
