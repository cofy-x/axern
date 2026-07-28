package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func runExternalTCPIngressBenchmark(natBackend, runtimeName, bpfnetPin, namespace, targetURL string, requests, concurrency, warmupRequests int, timeout time.Duration, startupSummary *natbench.StartupSummary, localitySummary *natbench.LocalitySummary) (natbench.Report, error) {
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

	summary, err := runHTTPBenchmarkFromNamespace(namespace, targetURL, requests, concurrency, warmupRequests, timeout)
	if err != nil {
		return report, err
	}

	cpuAfter, err := natbench.ReadCPUSnapshot()
	if err != nil {
		return report, err
	}

	path := natbench.PathBenchmark{
		Name:           "external_tcp_ingress",
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

func runHTTPBenchmarkFromNamespace(namespace, targetURL string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	self, err := os.Executable()
	if err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("resolve verify-nginx binary: %w", err)
	}
	cmd := exec.Command(
		"ip", "netns", "exec", namespace,
		self,
		"-mode", "http-benchmark-client",
		"-target-url", targetURL,
		"-benchmark-requests", fmt.Sprintf("%d", requests),
		"-benchmark-concurrency", fmt.Sprintf("%d", concurrency),
		"-benchmark-warmup-requests", fmt.Sprintf("%d", warmupRequests),
		"-timeout", timeout.String(),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	var summary natbench.WorkloadSummary
	if err := json.Unmarshal(output, &summary); err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("decode http benchmark summary: %w", err)
	}
	return summary, nil
}

func runHTTPBenchmarkClient(targetURL string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	if targetURL == "" {
		return natbench.WorkloadSummary{}, fmt.Errorf("target url is required")
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	return natbench.RunWorkloadWarmup(requests, concurrency, warmupRequests, func() error {
		resp, err := client.Get(targetURL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected http status %d", resp.StatusCode)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}), nil
}
