package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func readBenchmarkSummary(path string) (natbench.WorkloadSummary, error) {
	var summary natbench.WorkloadSummary
	data, err := os.ReadFile(path)
	if err != nil {
		return summary, err
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return summary, err
	}
	return summary, nil
}

type egressPathSnapshots struct {
	CPUBefore      natbench.CPUSnapshot
	CPUAfter       natbench.CPUSnapshot
	ClientBefore   natbench.ClientResourceSnapshot
	ClientAfter    natbench.ClientResourceSnapshot
	ClientPeak     natbench.ClientResourceSnapshot
	ClientSamples  uint64
	StatusBefore   bpfnet.Status
	StatusAfter    bpfnet.Status
	SNATMapPeak    bpfnet.SNATMapStats
	SNATMapSamples uint64
	StatusPostGC   bpfnet.Status
}

func buildEgressPathBenchmark(natBackend string, spec benchmarkTransportSpec, snapshots egressPathSnapshots, summary natbench.WorkloadSummary) natbench.PathBenchmark {
	path := natbench.PathBenchmark{
		Name:            spec.Name,
		Datapath:        natBackend,
		Summary:         summary,
		HostCPUPercent:  natbench.CPUUsagePercent(snapshots.CPUBefore, snapshots.CPUAfter),
		ClientBefore:    snapshots.ClientBefore,
		ClientAfter:     snapshots.ClientAfter,
		ClientPeak:      snapshots.ClientPeak,
		ClientDelta:     natbench.ClientResourcesDelta(snapshots.ClientBefore, snapshots.ClientAfter),
		ClientPeakDelta: natbench.ClientResourcesDelta(snapshots.ClientBefore, snapshots.ClientPeak),
		ClientSamples:   snapshots.ClientSamples,
		Notes: append([]string{
			"container egress benchmark via external netns responder",
			"kernel counters sampled only for this transport",
			"benchmark transport runs inside a preflighted suite sandbox",
			"client resource counters sampled inside the benchmark suite sandbox namespace",
		}, spec.Notes...),
		ObservedAt: time.Now().UTC(),
	}
	if natBackend == config.NatBackendEBPF {
		path.Mode = snapshots.StatusAfter.State.Mode
		path.Attachment = snapshots.StatusAfter.Attachment
		path.KernelBefore = snapshots.StatusBefore.Kernel
		path.KernelAfter = snapshots.StatusAfter.Kernel
		path.KernelDelta = bpfnetstatus.KernelDelta(snapshots.StatusBefore.Kernel, snapshots.StatusAfter.Kernel)
		path.SNATMapBefore = snapshots.StatusBefore.SNATMaps
		path.SNATMapAfter = snapshots.StatusAfter.SNATMaps
		path.SNATMapDelta = natbench.SNATMapDelta(snapshots.StatusBefore.SNATMaps, snapshots.StatusAfter.SNATMaps)
		path.SNATMapPeak = snapshots.SNATMapPeak
		path.SNATMapPeakDelta = natbench.SNATMapDelta(snapshots.StatusBefore.SNATMaps, snapshots.SNATMapPeak)
		path.SNATMapSamples = snapshots.SNATMapSamples
		path.SNATMapPostGC = snapshots.StatusPostGC.SNATMaps
		path.SNATMapGCReleased = natbench.SNATMapReleased(snapshots.StatusAfter.SNATMaps, snapshots.StatusPostGC.SNATMaps)
		path.Profile = natbench.BuildSNATProfile(summary, path.KernelDelta)
	}
	return path
}

func collectBenchmarkStreamPaths(specs []benchmarkTransportSpec, natBackend, bpfnetPinPath, stdoutPath string, streamWaitTimeout, snatPostGCWait time.Duration, waitCh <-chan probeWaitResult) ([]natbench.PathBenchmark, *probeWaitResult, error) {
	specByTransport := make(map[string]benchmarkTransportSpec, len(specs))
	for _, spec := range specs {
		specByTransport[spec.Transport] = spec
	}

	beforeCPU := make(map[string]natbench.CPUSnapshot, len(specs))
	beforeClient := make(map[string]natbench.ClientResourceSnapshot, len(specs))
	beforeStatus := make(map[string]bpfnet.Status, len(specs))
	snatSamplers := make(map[string]*snatMapSampler, len(specs))
	pathByTransport := make(map[string]natbench.PathBenchmark, len(specs))
	processedLines := 0
	containerExited := false
	var waitResult *probeWaitResult
	if streamWaitTimeout <= 0 {
		streamWaitTimeout = waitTimeout
	}
	deadline := time.Now().Add(streamWaitTimeout)
	defer func() {
		for _, sampler := range snatSamplers {
			sampler.Stop(bpfnet.SNATMapStats{})
		}
	}()

	for len(pathByTransport) < len(specs) {
		data, err := os.ReadFile(stdoutPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, waitResult, fmt.Errorf("read benchmark stream stdout: %w", err)
		}
		lines := splitNonEmptyLines(string(data))
		for processedLines < len(lines) {
			line := lines[processedLines]
			processedLines++

			var event natbench.BenchmarkStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				return nil, waitResult, fmt.Errorf("decode benchmark stream event %q: %w", line, err)
			}
			spec, ok := specByTransport[event.Transport]
			if !ok {
				return nil, waitResult, fmt.Errorf("unexpected benchmark transport %q", event.Transport)
			}

			switch event.Event {
			case natbench.BenchmarkPhaseBeginEvent:
				if event.Client == nil {
					return nil, waitResult, fmt.Errorf("benchmark phase %s begin missing client resource snapshot", event.Transport)
				}
				beforeClient[event.Transport] = *event.Client
				cpuSnapshot, err := natbench.ReadCPUSnapshot()
				if err != nil {
					return nil, waitResult, fmt.Errorf("read pre-benchmark cpu snapshot for %s: %w", event.Transport, err)
				}
				beforeCPU[event.Transport] = cpuSnapshot
				if natBackend == config.NatBackendEBPF {
					status, err := bpfnetstatus.Load(bpfnetPinPath)
					if err != nil {
						return nil, waitResult, fmt.Errorf("load pre-benchmark bpfnet status for %s: %w", event.Transport, err)
					}
					beforeStatus[event.Transport] = status
					snatSamplers[event.Transport] = startSNATMapSampler(bpfnetPinPath, time.Second)
				}
			case natbench.BenchmarkPhaseEndEvent:
				if event.Summary == nil {
					return nil, waitResult, fmt.Errorf("benchmark phase %s missing summary payload", event.Transport)
				}
				cpuBefore, ok := beforeCPU[event.Transport]
				if !ok {
					return nil, waitResult, fmt.Errorf("benchmark phase %s ended before begin snapshot", event.Transport)
				}
				clientBefore, ok := beforeClient[event.Transport]
				if !ok {
					return nil, waitResult, fmt.Errorf("benchmark phase %s ended before client resource snapshot", event.Transport)
				}
				if event.Client == nil {
					return nil, waitResult, fmt.Errorf("benchmark phase %s end missing client resource snapshot", event.Transport)
				}
				if event.ClientPeak == nil {
					return nil, waitResult, fmt.Errorf("benchmark phase %s end missing client resource peak snapshot", event.Transport)
				}
				clientAfter := *event.Client
				clientPeak := *event.ClientPeak
				cpuAfter, err := natbench.ReadCPUSnapshot()
				if err != nil {
					return nil, waitResult, fmt.Errorf("read post-benchmark cpu snapshot for %s: %w", event.Transport, err)
				}
				var statusBeforeValue bpfnet.Status
				var statusAfterValue bpfnet.Status
				var statusPostGCValue bpfnet.Status
				var snatMapPeak bpfnet.SNATMapStats
				var snatMapSamples uint64
				if natBackend == config.NatBackendEBPF {
					statusBeforeValue = beforeStatus[event.Transport]
					statusAfterValue, err = bpfnetstatus.Load(bpfnetPinPath)
					if err != nil {
						return nil, waitResult, fmt.Errorf("load post-benchmark bpfnet status for %s: %w", event.Transport, err)
					}
					if err := assertSNATStats(bpfnetPinPath); err != nil {
						return nil, waitResult, err
					}
					if sampler := snatSamplers[event.Transport]; sampler != nil {
						snatMapPeak, snatMapSamples = sampler.Stop(statusAfterValue.SNATMaps)
						delete(snatSamplers, event.Transport)
					} else {
						snatMapPeak = statusAfterValue.SNATMaps
					}
					statusPostGCValue = statusAfterValue
					if snatPostGCWait > 0 {
						time.Sleep(snatPostGCWait)
						statusPostGCValue, err = bpfnetstatus.Load(bpfnetPinPath)
						if err != nil {
							return nil, waitResult, fmt.Errorf("load post-GC bpfnet status for %s: %w", event.Transport, err)
						}
					}
				}
				pathByTransport[event.Transport] = buildEgressPathBenchmark(natBackend, spec, egressPathSnapshots{
					CPUBefore:      cpuBefore,
					CPUAfter:       cpuAfter,
					ClientBefore:   clientBefore,
					ClientAfter:    clientAfter,
					ClientPeak:     clientPeak,
					ClientSamples:  event.ClientSamples,
					StatusBefore:   statusBeforeValue,
					StatusAfter:    statusAfterValue,
					SNATMapPeak:    snatMapPeak,
					SNATMapSamples: snatMapSamples,
					StatusPostGC:   statusPostGCValue,
				}, *event.Summary)
			default:
				return nil, waitResult, fmt.Errorf("unexpected benchmark stream event %q", event.Event)
			}
		}

		select {
		case result := <-waitCh:
			if result.err != nil {
				return nil, &result, result.err
			}
			waitResult = &result
			containerExited = true
		default:
		}

		if len(pathByTransport) == len(specs) {
			break
		}
		if containerExited {
			break
		}
		if time.Now().After(deadline) {
			return nil, waitResult, fmt.Errorf("timed out waiting for benchmark stream events in %s", stdoutPath)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(pathByTransport) != len(specs) {
		return nil, waitResult, fmt.Errorf("benchmark stream incomplete: got %d/%d transport summaries", len(pathByTransport), len(specs))
	}

	paths := make([]natbench.PathBenchmark, 0, len(specs))
	for _, spec := range specs {
		path, ok := pathByTransport[spec.Transport]
		if !ok {
			return nil, waitResult, fmt.Errorf("missing benchmark result for transport %s", spec.Transport)
		}
		paths = append(paths, path)
	}
	return paths, waitResult, nil
}

func splitNonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func appendTransportSuffix(path, suffix string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	if ext == "" {
		return base + "-" + suffix
	}
	return base + "-" + suffix + ext
}

func sanitizeTransport(transport string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(transport)), "-", "_")
}
