package main

import (
	"fmt"
	"net"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func runExternalUDPIngressBenchmark(natBackend, runtimeName, bpfnetPin, namespace, host string, listenPort, requests, concurrency, warmupRequests int, timeout time.Duration, startupSummary *natbench.StartupSummary, localitySummary *natbench.LocalitySummary) (natbench.Report, error) {
	report := natbench.Report{
		Runtime:    runtimeName,
		NATBackend: natBackend,
		StartedAt:  time.Now().UTC(),
		Startup:    startupSummary,
		Locality:   localitySummary,
	}
	cpuBefore, err := natbench.ReadCPUSnapshot()
	if err != nil {
		return report, err
	}
	var before, after bpfnet.Status
	if natBackend == config.NatBackendEBPF {
		before, err = verifyutil.LoadBPFNetStatus(bpfnetPin)
		if err != nil {
			return report, err
		}
	}
	target := net.JoinHostPort(host, fmt.Sprintf("%d", listenPort))
	summary, err := runBenchmarkProbeFromNamespace(namespace, target, requests, concurrency, warmupRequests, timeout)
	if err != nil {
		return report, err
	}
	cpuAfter, err := natbench.ReadCPUSnapshot()
	if err != nil {
		return report, err
	}
	path := natbench.PathBenchmark{
		Name:           "external_udp_ingress",
		Datapath:       natBackend,
		Summary:        summary,
		HostCPUPercent: natbench.CPUUsagePercent(cpuBefore, cpuAfter),
		ObservedAt:     time.Now().UTC(),
		Notes:          []string{"truth-path external netns benchmark"},
	}
	if natBackend == config.NatBackendEBPF {
		after, err = verifyutil.LoadBPFNetStatus(bpfnetPin)
		if err != nil {
			return report, err
		}
		path.Mode = after.State.Mode
		path.Attachment = after.Attachment
		path.KernelBefore = before.Kernel
		path.KernelAfter = after.Kernel
		path.KernelDelta = bpfnetstatus.KernelDelta(before.Kernel, after.Kernel)
	}
	report.Paths = []natbench.PathBenchmark{path}
	report.CompletedAt = time.Now().UTC()
	return report, nil
}
