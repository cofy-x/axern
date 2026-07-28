package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

type benchmarkTransportSpec struct {
	Name      string
	Transport string
	Notes     []string
}

type probeWaitResult struct {
	stdout []byte
	stderr []byte
	err    error
}

const (
	defaultBenchmarkPhaseDelay     = 250 * time.Millisecond
	multiClientBenchmarkPhaseDelay = 5 * time.Second
)

func runBenchmarkReport(clients *verifyutil.NodeClients, baseSpec *privatenodev1.ResolvedExecutionConfig, runtimeID, stdoutPath, stderrPath, runtimeName, natBackend, bpfnetPinPath, rootfs, tcpAddress, udpAddress, externalAddress, expectedSourceIP string, timeout time.Duration, requests, concurrency, warmupRequests, clientCount int, snatPostGCWait time.Duration, benchmarkTransports string) (natbench.Report, error) {
	report := natbench.Report{
		Runtime:    runtimeName,
		NATBackend: natBackend,
		StartedAt:  time.Now().UTC(),
	}
	startupBefore, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", runtimeName, "local")
	if err != nil {
		return natbench.Report{}, err
	}

	verifyStdout := appendTransportSuffix(stdoutPath, "verify")
	verifyStderr := appendTransportSuffix(stderrPath, "verify")
	verifyOutput, verifyErrOutput, err := runProbeContainer(
		clients,
		baseSpec,
		runtimeID+"-verify",
		verifyStdout,
		verifyStderr,
		buildProbeCommand("verify", "", "", tcpAddress, udpAddress, externalAddress, expectedSourceIP, timeout, 0, 0, 0, 0),
	)
	if err != nil {
		return natbench.Report{}, err
	}
	if len(strings.TrimSpace(string(verifyErrOutput))) > 0 {
		fmt.Printf("egress_probe_stderr=%s\n", strings.TrimSpace(string(verifyErrOutput)))
	}
	if !strings.Contains(string(verifyOutput), "egress_probe_ok=true") {
		return natbench.Report{}, fmt.Errorf("egress verify preflight missing success marker: %s", strings.TrimSpace(string(verifyOutput)))
	}
	if natBackend == config.NatBackendEBPF {
		if err := assertSNATStats(bpfnetPinPath); err != nil {
			return natbench.Report{}, err
		}
	}
	if clientCount <= 0 {
		clientCount = 1
	}

	specs, err := selectBenchmarkTransportSpecs(benchmarkTransports)
	if err != nil {
		return natbench.Report{}, err
	}
	specs, err = applyBenchmarkClientCount(specs, clientCount)
	if err != nil {
		return natbench.Report{}, err
	}
	if err := validateBenchmarkPostGCSnapshot(natBackend, snatPostGCWait, specs); err != nil {
		return natbench.Report{}, err
	}
	if len(specs) == 0 {
		startupAfter, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", runtimeName, "local")
		if err != nil {
			return natbench.Report{}, err
		}
		report.Startup = natbench.DiffStartupSummary(startupBefore, startupAfter)
		report.Locality, err = natbench.CaptureLocalitySummary(
			"http://127.0.0.1:23001/inventoryz",
			natbench.LocalRootfsKey(rootfs),
		)
		if err != nil {
			return natbench.Report{}, err
		}
		report.CompletedAt = time.Now().UTC()
		return report, nil
	}

	streamTransports := make([]string, 0, len(specs))
	for _, spec := range specs {
		streamTransports = append(streamTransports, spec.Transport)
	}

	var paths []natbench.PathBenchmark
	if clientCount == 1 {
		streamTransportArg := strings.Join(streamTransports, ",")

		streamStdout := appendTransportSuffix(stdoutPath, "suite")
		streamStderr := appendTransportSuffix(stderrPath, "suite")
		streamWaitTimeout := benchmarkSuiteWaitTimeout(timeout, requests, concurrency, len(specs))
		streamHandle, err := startProbeContainer(
			clients,
			baseSpec,
			runtimeID+"-suite",
			streamStdout,
			streamStderr,
			buildProbeCommand("benchmark-suite-stream", "", streamTransportArg, tcpAddress, udpAddress, externalAddress, expectedSourceIP, timeout, requests, concurrency, warmupRequests, defaultBenchmarkPhaseDelay),
		)
		if err != nil {
			return natbench.Report{}, err
		}
		defer streamHandle.cleanup()

		waitCh := make(chan probeWaitResult, 1)
		go func() {
			stdoutData, stderrData, err := streamHandle.wait(streamWaitTimeout)
			waitCh <- probeWaitResult{stdout: stdoutData, stderr: stderrData, err: err}
		}()

		waitResult := (*probeWaitResult)(nil)
		paths, waitResult, err = collectBenchmarkStreamPaths(specs, natBackend, bpfnetPinPath, streamStdout, streamWaitTimeout, snatPostGCWait, waitCh)
		if err != nil {
			return natbench.Report{}, err
		}
		if waitResult == nil {
			result := <-waitCh
			waitResult = &result
		}
		if waitResult.err != nil {
			return natbench.Report{}, waitResult.err
		}
		if len(strings.TrimSpace(string(waitResult.stderr))) > 0 {
			fmt.Printf("egress_probe_stderr=%s\n", strings.TrimSpace(string(waitResult.stderr)))
		}
	} else {
		paths = make([]natbench.PathBenchmark, 0, len(specs))
		for _, spec := range specs {
			path, err := runMultiClientBenchmarkPath(clients, baseSpec, runtimeID, stdoutPath, stderrPath, natBackend, bpfnetPinPath, tcpAddress, udpAddress, externalAddress, expectedSourceIP, timeout, requests, concurrency, warmupRequests, clientCount, snatPostGCWait, spec)
			if err != nil {
				return natbench.Report{}, err
			}
			paths = append(paths, path)
		}
	}

	startupAfter, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", runtimeName, "local")
	if err != nil {
		return natbench.Report{}, err
	}
	report.Startup = natbench.DiffStartupSummary(startupBefore, startupAfter)
	report.Locality, err = natbench.CaptureLocalitySummary(
		"http://127.0.0.1:23001/inventoryz",
		natbench.LocalRootfsKey(rootfs),
	)
	if err != nil {
		return natbench.Report{}, err
	}
	report.Paths = paths
	report.CompletedAt = time.Now().UTC()
	return report, nil
}

func benchmarkSuiteWaitTimeout(probeTimeout time.Duration, requests, concurrency, transports int) time.Duration {
	timeout := waitTimeout
	if probeTimeout > 0 && probeTimeout*2 > timeout {
		timeout = probeTimeout * 2
	}
	if requests <= 0 || concurrency <= 0 || transports <= 0 {
		return timeout
	}

	scaled := 2*time.Minute + time.Duration(transports)*time.Duration(requests/1000+1)*time.Second
	if scaled > timeout {
		timeout = scaled
	}
	const maxBenchmarkSuiteWaitTimeout = 30 * time.Minute
	if timeout > maxBenchmarkSuiteWaitTimeout {
		return maxBenchmarkSuiteWaitTimeout
	}
	return timeout
}

func buildProbeCommand(mode, transport, benchmarkTransports, tcpAddress, udpAddress, icmpAddress, expectedSourceIP string, timeout time.Duration, requests, concurrency, warmupRequests int, phaseDelay time.Duration) []string {
	command := []string{
		"/axnoded-bin/egress-probe",
		"-mode", mode,
		"-tcp-address", tcpAddress,
		"-udp-address", udpAddress,
		"-icmp-address", icmpAddress,
		"-expected-source-ip", expectedSourceIP,
		"-timeout", timeout.String(),
	}
	if transport != "" {
		command = append(command, "-transport", transport)
	}
	if benchmarkTransports != "" {
		command = append(command, "-benchmark-transports", benchmarkTransports)
	}
	if requests > 0 {
		command = append(command,
			"-requests", fmt.Sprintf("%d", requests),
			"-concurrency", fmt.Sprintf("%d", concurrency),
			"-warmup-requests", fmt.Sprintf("%d", warmupRequests),
		)
	}
	if phaseDelay > 0 {
		command = append(command, "-phase-delay", phaseDelay.String())
	}
	return command
}

func selectBenchmarkTransportSpecs(raw string) ([]benchmarkTransportSpec, error) {
	known := map[string]benchmarkTransportSpec{
		"tcp-short": {
			Name:      "egress_tcp_short",
			Transport: "tcp-short",
			Notes:     []string{"each request opens a new TCP connection to measure short-connection churn"},
		},
		"tcp-reuse": {
			Name:      "egress_tcp_reuse",
			Transport: "tcp-reuse",
			Notes:     []string{"each worker reuses a single TCP connection to isolate steady-state dataplane cost"},
		},
		"tcp-pool": {
			Name:      "egress_tcp_pool",
			Transport: "tcp-pool",
			Notes:     []string{"each worker reuses a small TCP connection pool to model service-client connection reuse"},
		},
		"udp": {
			Name:      "egress_udp",
			Transport: "udp",
		},
		"udp-connected": {
			Name:      "egress_udp_connected",
			Transport: "udp-connected",
			Notes:     []string{"each worker reuses a single UDP socket to isolate steady-state NAT cost"},
		},
	}
	if strings.TrimSpace(raw) == "" {
		return []benchmarkTransportSpec{
			known["tcp-short"],
		}, nil
	}

	seen := make(map[string]struct{})
	specs := make([]benchmarkTransportSpec, 0, 3)
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(strings.ToLower(part))
		if key == "" {
			continue
		}
		spec, ok := known[key]
		if !ok {
			return nil, fmt.Errorf("unsupported benchmark transport %q", part)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		specs = append(specs, spec)
	}
	return specs, nil
}

func applyBenchmarkClientCount(specs []benchmarkTransportSpec, clientCount int) ([]benchmarkTransportSpec, error) {
	if clientCount <= 1 {
		return specs, nil
	}
	out := make([]benchmarkTransportSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Transport != "tcp-short" {
			return nil, fmt.Errorf("benchmark-client-count > 1 is only supported for tcp-short; got %s", spec.Transport)
		}
		spec.Name = "egress_tcp_short_multi_client"
		spec.Notes = append(spec.Notes,
			fmt.Sprintf("short-connection churn split across %d concurrent client sandboxes", clientCount),
			"multi-client latency percentiles use the maximum percentile observed across client sandboxes",
		)
		out = append(out, spec)
	}
	return out, nil
}

func validateBenchmarkPostGCSnapshot(natBackend string, snatPostGCWait time.Duration, specs []benchmarkTransportSpec) error {
	if natBackend != config.NatBackendEBPF || snatPostGCWait <= 0 || len(specs) <= 1 {
		return nil
	}
	transports := make([]string, 0, len(specs))
	for _, spec := range specs {
		transports = append(transports, spec.Transport)
	}
	return fmt.Errorf("benchmark-snat-post-gc-wait requires a single eBPF benchmark transport; got %s", strings.Join(transports, ","))
}

type multiClientStreamResult struct {
	summary       natbench.WorkloadSummary
	clientBefore  natbench.ClientResourceSnapshot
	clientAfter   natbench.ClientResourceSnapshot
	clientPeak    natbench.ClientResourceSnapshot
	clientSamples uint64
}

func runMultiClientBenchmarkPath(clients *verifyutil.NodeClients, baseSpec *privatenodev1.ResolvedExecutionConfig, runtimeID, stdoutPath, stderrPath, natBackend, bpfnetPinPath, tcpAddress, udpAddress, externalAddress, expectedSourceIP string, timeout time.Duration, requests, concurrency, warmupRequests, clientCount int, snatPostGCWait time.Duration, spec benchmarkTransportSpec) (natbench.PathBenchmark, error) {
	if spec.Transport != "tcp-short" {
		return natbench.PathBenchmark{}, fmt.Errorf("multi-client benchmark only supports tcp-short; got %s", spec.Transport)
	}
	if clientCount <= 1 {
		return natbench.PathBenchmark{}, fmt.Errorf("multi-client benchmark requires client count > 1")
	}
	if requests < clientCount {
		return natbench.PathBenchmark{}, fmt.Errorf("benchmark requests %d must be >= client count %d", requests, clientCount)
	}
	if concurrency < clientCount {
		return natbench.PathBenchmark{}, fmt.Errorf("benchmark concurrency %d must be >= client count %d", concurrency, clientCount)
	}

	requestsByClient := splitEvenly(requests, clientCount)
	concurrencyByClient := splitEvenly(concurrency, clientCount)
	warmupByClient := splitEvenly(warmupRequests, clientCount)

	cpuBefore, err := natbench.ReadCPUSnapshot()
	if err != nil {
		return natbench.PathBenchmark{}, fmt.Errorf("read pre-benchmark cpu snapshot for %s: %w", spec.Transport, err)
	}
	var statusBefore bpfnet.Status
	var snatSampler *snatMapSampler
	if natBackend == config.NatBackendEBPF {
		statusBefore, err = bpfnetstatus.Load(bpfnetPinPath)
		if err != nil {
			return natbench.PathBenchmark{}, fmt.Errorf("load pre-benchmark bpfnet status for %s: %w", spec.Transport, err)
		}
		snatSampler = startSNATMapSampler(bpfnetPinPath, time.Second)
		defer func() {
			if snatSampler != nil {
				snatSampler.Stop(bpfnet.SNATMapStats{})
			}
		}()
	}

	handles := make([]*probeContainerHandle, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		suffix := fmt.Sprintf("%s-client-%02d", sanitizeTransport(spec.Transport), i+1)
		handle, err := startProbeContainer(
			clients,
			baseSpec,
			fmt.Sprintf("%s-suite-%s", runtimeID, suffix),
			appendTransportSuffix(stdoutPath, suffix),
			appendTransportSuffix(stderrPath, suffix),
			buildProbeCommand("benchmark-suite-stream", "", spec.Transport, tcpAddress, udpAddress, externalAddress, expectedSourceIP, timeout, requestsByClient[i], concurrencyByClient[i], warmupByClient[i], multiClientBenchmarkPhaseDelay),
		)
		if err != nil {
			for _, existing := range handles {
				existing.cleanup()
			}
			return natbench.PathBenchmark{}, err
		}
		handles = append(handles, handle)
	}
	defer func() {
		for _, handle := range handles {
			handle.cleanup()
		}
	}()

	waitTimeout := benchmarkSuiteWaitTimeout(timeout, maxInt(requestsByClient...), maxInt(concurrencyByClient...), 1) + multiClientBenchmarkPhaseDelay
	results := make([]multiClientStreamResult, 0, clientCount)
	for i, handle := range handles {
		stdoutData, stderrData, err := handle.wait(waitTimeout)
		if len(strings.TrimSpace(string(stderrData))) > 0 {
			fmt.Printf("egress_probe_stderr_client_%02d=%s\n", i+1, strings.TrimSpace(string(stderrData)))
		}
		if err != nil {
			return natbench.PathBenchmark{}, err
		}
		result, err := parseBenchmarkStreamResult(stdoutData, spec.Transport)
		if err != nil {
			return natbench.PathBenchmark{}, fmt.Errorf("parse multi-client benchmark stream client %d: %w", i+1, err)
		}
		results = append(results, result)
	}

	cpuAfter, err := natbench.ReadCPUSnapshot()
	if err != nil {
		return natbench.PathBenchmark{}, fmt.Errorf("read post-benchmark cpu snapshot for %s: %w", spec.Transport, err)
	}
	var statusAfter bpfnet.Status
	var statusPostGC bpfnet.Status
	var snatMapPeak bpfnet.SNATMapStats
	var snatMapSamples uint64
	if natBackend == config.NatBackendEBPF {
		statusAfter, err = bpfnetstatus.Load(bpfnetPinPath)
		if err != nil {
			return natbench.PathBenchmark{}, fmt.Errorf("load post-benchmark bpfnet status for %s: %w", spec.Transport, err)
		}
		if err := assertSNATStats(bpfnetPinPath); err != nil {
			return natbench.PathBenchmark{}, err
		}
		if snatSampler != nil {
			snatMapPeak, snatMapSamples = snatSampler.Stop(statusAfter.SNATMaps)
			snatSampler = nil
		} else {
			snatMapPeak = statusAfter.SNATMaps
		}
		statusPostGC = statusAfter
		if snatPostGCWait > 0 {
			time.Sleep(snatPostGCWait)
			statusPostGC, err = bpfnetstatus.Load(bpfnetPinPath)
			if err != nil {
				return natbench.PathBenchmark{}, fmt.Errorf("load post-GC bpfnet status for %s: %w", spec.Transport, err)
			}
		}
	}

	summaries := make([]natbench.WorkloadSummary, 0, len(results))
	clientBefore := make([]natbench.ClientResourceSnapshot, 0, len(results))
	clientAfter := make([]natbench.ClientResourceSnapshot, 0, len(results))
	clientPeak := make([]natbench.ClientResourceSnapshot, 0, len(results))
	var clientSamples uint64
	for _, result := range results {
		summaries = append(summaries, result.summary)
		clientBefore = append(clientBefore, result.clientBefore)
		clientAfter = append(clientAfter, result.clientAfter)
		clientPeak = append(clientPeak, result.clientPeak)
		clientSamples += result.clientSamples
	}

	path := buildEgressPathBenchmark(natBackend, spec, egressPathSnapshots{
		CPUBefore:      cpuBefore,
		CPUAfter:       cpuAfter,
		ClientBefore:   natbench.CombineClientResourceSnapshots(clientBefore),
		ClientAfter:    natbench.CombineClientResourceSnapshots(clientAfter),
		ClientPeak:     natbench.CombineClientResourceSnapshots(clientPeak),
		ClientSamples:  clientSamples,
		StatusBefore:   statusBefore,
		StatusAfter:    statusAfter,
		SNATMapPeak:    snatMapPeak,
		SNATMapSamples: snatMapSamples,
		StatusPostGC:   statusPostGC,
	}, natbench.CombineConcurrentWorkloadSummaries(summaries, concurrency))
	path.Notes = append(path.Notes,
		fmt.Sprintf("requests split by client: %v", requestsByClient),
		fmt.Sprintf("concurrency split by client: %v", concurrencyByClient),
	)
	return path, nil
}

func parseBenchmarkStreamResult(stdout []byte, transport string) (multiClientStreamResult, error) {
	var result multiClientStreamResult
	beginSeen := false
	endSeen := false
	for _, line := range splitNonEmptyLines(string(stdout)) {
		var event natbench.BenchmarkStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return multiClientStreamResult{}, fmt.Errorf("decode benchmark stream event %q: %w", line, err)
		}
		if event.Transport != transport {
			return multiClientStreamResult{}, fmt.Errorf("unexpected benchmark transport %q, want %q", event.Transport, transport)
		}
		switch event.Event {
		case natbench.BenchmarkPhaseBeginEvent:
			if event.Client == nil {
				return multiClientStreamResult{}, fmt.Errorf("benchmark phase %s begin missing client resource snapshot", event.Transport)
			}
			beginSeen = true
			result.clientBefore = *event.Client
		case natbench.BenchmarkPhaseEndEvent:
			if event.Summary == nil {
				return multiClientStreamResult{}, fmt.Errorf("benchmark phase %s missing summary payload", event.Transport)
			}
			if event.Client == nil {
				return multiClientStreamResult{}, fmt.Errorf("benchmark phase %s end missing client resource snapshot", event.Transport)
			}
			if event.ClientPeak == nil {
				return multiClientStreamResult{}, fmt.Errorf("benchmark phase %s end missing client resource peak snapshot", event.Transport)
			}
			endSeen = true
			result.summary = *event.Summary
			result.clientAfter = *event.Client
			result.clientPeak = *event.ClientPeak
			result.clientSamples = event.ClientSamples
		default:
			return multiClientStreamResult{}, fmt.Errorf("unexpected benchmark stream event %q", event.Event)
		}
	}
	if !beginSeen || !endSeen {
		return multiClientStreamResult{}, fmt.Errorf("benchmark stream incomplete: begin=%t end=%t", beginSeen, endSeen)
	}
	return result, nil
}

func splitEvenly(total, parts int) []int {
	if parts <= 0 {
		return nil
	}
	out := make([]int, parts)
	if total <= 0 {
		return out
	}
	base := total / parts
	rem := total % parts
	for i := range out {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
