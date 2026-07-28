package natbench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
)

type LatencySummary struct {
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
}

type WorkloadSummary struct {
	Requests        int            `json:"requests"`
	Concurrency     int            `json:"concurrency"`
	Successes       int            `json:"successes"`
	Failures        int            `json:"failures"`
	FirstError      string         `json:"firstError,omitempty"`
	DurationSeconds float64        `json:"durationSeconds"`
	ThroughputRPS   float64        `json:"throughputRps"`
	Latency         LatencySummary `json:"latency"`
}

type NATProfile struct {
	SNATMappingsPerSuccess  float64 `json:"snatMappingsPerSuccess,omitempty"`
	SNATForwardReuseRatio   float64 `json:"snatForwardReuseRatio,omitempty"`
	SNATReverseHitRatio     float64 `json:"snatReverseHitRatio,omitempty"`
	SNATFallbackRatio       float64 `json:"snatFallbackRatio,omitempty"`
	SNATAllocExhaustedRatio float64 `json:"snatAllocExhaustedRatio,omitempty"`
	SNATTCPNonSynMissRatio  float64 `json:"snatTcpNonSynMissRatio,omitempty"`
	SNATUDPSamePortRatio    float64 `json:"snatUdpSamePortRatio,omitempty"`
	SNATUDPPortRewriteRatio float64 `json:"snatUdpPortRewriteRatio,omitempty"`
	SNATUDPChecksumRatio    float64 `json:"snatUdpChecksumRatio,omitempty"`
}

type PathBenchmark struct {
	Name              string                     `json:"name"`
	Datapath          string                     `json:"datapath"`
	Mode              string                     `json:"mode,omitempty"`
	Summary           WorkloadSummary            `json:"summary"`
	Profile           NATProfile                 `json:"profile,omitempty"`
	HostCPUPercent    float64                    `json:"hostCpuPercent,omitempty"`
	ClientBefore      ClientResourceSnapshot     `json:"clientBefore,omitempty"`
	ClientAfter       ClientResourceSnapshot     `json:"clientAfter,omitempty"`
	ClientPeak        ClientResourceSnapshot     `json:"clientPeak,omitempty"`
	ClientDelta       ClientResourceDelta        `json:"clientDelta,omitempty"`
	ClientPeakDelta   ClientResourceDelta        `json:"clientPeakDelta,omitempty"`
	ClientSamples     uint64                     `json:"clientSamples,omitempty"`
	KernelBefore      bpfnet.KernelStats         `json:"kernelBefore,omitempty"`
	KernelAfter       bpfnet.KernelStats         `json:"kernelAfter,omitempty"`
	KernelDelta       bpfnet.KernelStats         `json:"kernelDelta,omitempty"`
	SNATMapBefore     bpfnet.SNATMapStats        `json:"snatMapBefore,omitempty"`
	SNATMapAfter      bpfnet.SNATMapStats        `json:"snatMapAfter,omitempty"`
	SNATMapDelta      bpfnet.SNATMapStats        `json:"snatMapDelta,omitempty"`
	SNATMapPeak       bpfnet.SNATMapStats        `json:"snatMapPeak,omitempty"`
	SNATMapPeakDelta  bpfnet.SNATMapStats        `json:"snatMapPeakDelta,omitempty"`
	SNATMapSamples    uint64                     `json:"snatMapSamples,omitempty"`
	SNATMapPostGC     bpfnet.SNATMapStats        `json:"snatMapPostGc,omitempty"`
	SNATMapGCReleased bpfnet.SNATMapStats        `json:"snatMapGcReleased,omitempty"`
	Attachment        bpfnet.AttachmentReadiness `json:"attachment,omitempty"`
	Notes             []string                   `json:"notes,omitempty"`
	ObservedAt        time.Time                  `json:"observedAt"`
}

type Report struct {
	Runtime     string           `json:"runtime"`
	NATBackend  string           `json:"natBackend"`
	Startup     *StartupSummary  `json:"startup,omitempty"`
	Locality    *LocalitySummary `json:"locality,omitempty"`
	StartedAt   time.Time        `json:"startedAt"`
	CompletedAt time.Time        `json:"completedAt"`
	Paths       []PathBenchmark  `json:"paths"`
}

type CPUSnapshot struct {
	total uint64
	idle  uint64
}

func RunWorkload(requests, concurrency int, probe func() error) WorkloadSummary {
	return RunWorkloadWarmup(requests, concurrency, 0, probe)
}

func RunWorkloadWarmup(requests, concurrency, warmupRequests int, probe func() error) WorkloadSummary {
	return RunWorkloadWithWorkerWarmup(requests, concurrency, warmupRequests, func() (func() error, func(), error) {
		return probe, func() {}, nil
	})
}

func RunWorkloadWithWorker(requests, concurrency int, setup func() (func() error, func(), error)) WorkloadSummary {
	return RunWorkloadWithWorkerWarmup(requests, concurrency, 0, setup)
}

func RunWorkloadWithWorkerWarmup(requests, concurrency, warmupRequests int, setup func() (func() error, func(), error)) WorkloadSummary {
	if requests <= 0 {
		return WorkloadSummary{}
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if warmupRequests < 0 {
		warmupRequests = 0
	}

	type result struct {
		latency time.Duration
		err     error
	}

	jobs := make(chan struct{}, requests)
	results := make(chan result, requests)
	startCh := make(chan struct{})
	var ready sync.WaitGroup

	var wg sync.WaitGroup
	workerCount := concurrency
	if workerCount > requests {
		workerCount = requests
	}
	ready.Add(workerCount)
	warmupsPerWorker := distributeWarmupRequests(warmupRequests, workerCount)
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func(warmups int) {
			defer wg.Done()
			probe, cleanup, setupErr := setup()
			if cleanup != nil {
				defer cleanup()
			}
			for i := 0; i < warmups && setupErr == nil; i++ {
				setupErr = probe()
			}
			ready.Done()
			<-startCh
			for range jobs {
				reqStart := time.Now()
				err := setupErr
				if err == nil {
					err = probe()
				}
				results <- result{
					latency: time.Since(reqStart),
					err:     err,
				}
			}
		}(warmupsPerWorker[worker])
	}

	ready.Wait()
	start := time.Now()
	close(startCh)
	for i := 0; i < requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	wg.Wait()
	close(results)

	latencies := make([]time.Duration, 0, requests)
	failures := 0
	firstError := ""
	for result := range results {
		if result.err != nil {
			failures++
			if firstError == "" {
				firstError = result.err.Error()
			}
			continue
		}
		latencies = append(latencies, result.latency)
	}

	summary := Summarize(requests, concurrency, len(latencies), failures, time.Since(start), latencies)
	summary.FirstError = firstError
	return summary
}

func distributeWarmupRequests(warmupRequests, workerCount int) []int {
	if workerCount <= 0 {
		return nil
	}
	distribution := make([]int, workerCount)
	for i := 0; i < warmupRequests; i++ {
		distribution[i%workerCount]++
	}
	return distribution
}

func Summarize(requests, concurrency, successes, failures int, total time.Duration, latencies []time.Duration) WorkloadSummary {
	summary := WorkloadSummary{
		Requests:        requests,
		Concurrency:     concurrency,
		Successes:       successes,
		Failures:        failures,
		DurationSeconds: total.Seconds(),
	}
	if total > 0 {
		summary.ThroughputRPS = float64(successes) / total.Seconds()
	}
	if len(latencies) == 0 {
		return summary
	}

	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	summary.Latency = LatencySummary{
		P50Ms: durationAtPercentile(sorted, 0.50).Seconds() * 1000,
		P95Ms: durationAtPercentile(sorted, 0.95).Seconds() * 1000,
		P99Ms: durationAtPercentile(sorted, 0.99).Seconds() * 1000,
	}
	return summary
}

func CombineConcurrentWorkloadSummaries(samples []WorkloadSummary, concurrency int) WorkloadSummary {
	if len(samples) == 0 {
		return WorkloadSummary{}
	}
	var summary WorkloadSummary
	for _, sample := range samples {
		summary.Requests += sample.Requests
		summary.Successes += sample.Successes
		summary.Failures += sample.Failures
		if summary.FirstError == "" {
			summary.FirstError = sample.FirstError
		}
		if sample.DurationSeconds > summary.DurationSeconds {
			summary.DurationSeconds = sample.DurationSeconds
		}
		summary.Latency.P50Ms = maxFloat64(summary.Latency.P50Ms, sample.Latency.P50Ms)
		summary.Latency.P95Ms = maxFloat64(summary.Latency.P95Ms, sample.Latency.P95Ms)
		summary.Latency.P99Ms = maxFloat64(summary.Latency.P99Ms, sample.Latency.P99Ms)
	}
	summary.Concurrency = concurrency
	if summary.DurationSeconds > 0 {
		summary.ThroughputRPS = float64(summary.Successes) / summary.DurationSeconds
	}
	return summary
}

func BuildSNATProfile(summary WorkloadSummary, kernel bpfnet.KernelStats) NATProfile {
	var profile NATProfile
	if summary.Successes > 0 {
		profile.SNATMappingsPerSuccess = float64(kernel.SNATMappingsProgrammed) / float64(summary.Successes)
	}
	if kernel.SNATHits > 0 {
		profile.SNATForwardReuseRatio = float64(kernel.SNATFwdHits) / float64(kernel.SNATHits)
		profile.SNATReverseHitRatio = float64(kernel.SNATRevHits) / float64(kernel.SNATHits)
		profile.SNATUDPSamePortRatio = float64(kernel.SNATUDPSamePortHits) / float64(kernel.SNATHits)
		profile.SNATUDPPortRewriteRatio = float64(kernel.SNATUDPPortRewriteHits) / float64(kernel.SNATHits)
		profile.SNATUDPChecksumRatio = float64(kernel.SNATUDPChecksumHits) / float64(kernel.SNATHits)
	}
	snatPackets := kernel.SNATHits + kernel.SNATFallbackHits
	if snatPackets > 0 {
		profile.SNATFallbackRatio = float64(kernel.SNATFallbackHits) / float64(snatPackets)
		profile.SNATTCPNonSynMissRatio = float64(kernel.SNATTCPNonSynMisses) / float64(snatPackets)
	}
	allocDecisions := kernel.SNATMappingsProgrammed + kernel.SNATAllocExhausted
	if allocDecisions > 0 {
		profile.SNATAllocExhaustedRatio = float64(kernel.SNATAllocExhausted) / float64(allocDecisions)
	}
	return profile
}

func SNATMapDelta(before, after bpfnet.SNATMapStats) bpfnet.SNATMapStats {
	return bpfnet.SNATMapStats{
		FwdEntries:             saturatingUint64Delta(before.FwdEntries, after.FwdEntries),
		FwdTCPEntries:          saturatingUint64Delta(before.FwdTCPEntries, after.FwdTCPEntries),
		FwdUDPEntries:          saturatingUint64Delta(before.FwdUDPEntries, after.FwdUDPEntries),
		FwdICMPEntries:         saturatingUint64Delta(before.FwdICMPEntries, after.FwdICMPEntries),
		FwdActiveEntries:       saturatingUint64Delta(before.FwdActiveEntries, after.FwdActiveEntries),
		FwdClosingEntries:      saturatingUint64Delta(before.FwdClosingEntries, after.FwdClosingEntries),
		FwdOrigClosingEntries:  saturatingUint64Delta(before.FwdOrigClosingEntries, after.FwdOrigClosingEntries),
		FwdReplyClosingEntries: saturatingUint64Delta(before.FwdReplyClosingEntries, after.FwdReplyClosingEntries),
		FwdFullClosingEntries:  saturatingUint64Delta(before.FwdFullClosingEntries, after.FwdFullClosingEntries),
		RevEntries:             saturatingUint64Delta(before.RevEntries, after.RevEntries),
		RevTCPEntries:          saturatingUint64Delta(before.RevTCPEntries, after.RevTCPEntries),
		RevUDPEntries:          saturatingUint64Delta(before.RevUDPEntries, after.RevUDPEntries),
		RevICMPEntries:         saturatingUint64Delta(before.RevICMPEntries, after.RevICMPEntries),
		RevActiveEntries:       saturatingUint64Delta(before.RevActiveEntries, after.RevActiveEntries),
		RevClosingEntries:      saturatingUint64Delta(before.RevClosingEntries, after.RevClosingEntries),
		RevOrigClosingEntries:  saturatingUint64Delta(before.RevOrigClosingEntries, after.RevOrigClosingEntries),
		RevReplyClosingEntries: saturatingUint64Delta(before.RevReplyClosingEntries, after.RevReplyClosingEntries),
		RevFullClosingEntries:  saturatingUint64Delta(before.RevFullClosingEntries, after.RevFullClosingEntries),
		RevReverseEntries:      saturatingUint64Delta(before.RevReverseEntries, after.RevReverseEntries),
		RevAliasEntries:        saturatingUint64Delta(before.RevAliasEntries, after.RevAliasEntries),
		TranslatedPortsUsed:    saturatingUint64Delta(before.TranslatedPortsUsed, after.TranslatedPortsUsed),
		UDPTranslatedPortsUsed: saturatingUint64Delta(before.UDPTranslatedPortsUsed, after.UDPTranslatedPortsUsed),
	}
}

func SNATMapReleased(beforeGC, afterGC bpfnet.SNATMapStats) bpfnet.SNATMapStats {
	return bpfnet.SNATMapStats{
		FwdEntries:             saturatingUint64Delta(afterGC.FwdEntries, beforeGC.FwdEntries),
		FwdTCPEntries:          saturatingUint64Delta(afterGC.FwdTCPEntries, beforeGC.FwdTCPEntries),
		FwdUDPEntries:          saturatingUint64Delta(afterGC.FwdUDPEntries, beforeGC.FwdUDPEntries),
		FwdICMPEntries:         saturatingUint64Delta(afterGC.FwdICMPEntries, beforeGC.FwdICMPEntries),
		FwdActiveEntries:       saturatingUint64Delta(afterGC.FwdActiveEntries, beforeGC.FwdActiveEntries),
		FwdClosingEntries:      saturatingUint64Delta(afterGC.FwdClosingEntries, beforeGC.FwdClosingEntries),
		FwdOrigClosingEntries:  saturatingUint64Delta(afterGC.FwdOrigClosingEntries, beforeGC.FwdOrigClosingEntries),
		FwdReplyClosingEntries: saturatingUint64Delta(afterGC.FwdReplyClosingEntries, beforeGC.FwdReplyClosingEntries),
		FwdFullClosingEntries:  saturatingUint64Delta(afterGC.FwdFullClosingEntries, beforeGC.FwdFullClosingEntries),
		RevEntries:             saturatingUint64Delta(afterGC.RevEntries, beforeGC.RevEntries),
		RevTCPEntries:          saturatingUint64Delta(afterGC.RevTCPEntries, beforeGC.RevTCPEntries),
		RevUDPEntries:          saturatingUint64Delta(afterGC.RevUDPEntries, beforeGC.RevUDPEntries),
		RevICMPEntries:         saturatingUint64Delta(afterGC.RevICMPEntries, beforeGC.RevICMPEntries),
		RevActiveEntries:       saturatingUint64Delta(afterGC.RevActiveEntries, beforeGC.RevActiveEntries),
		RevClosingEntries:      saturatingUint64Delta(afterGC.RevClosingEntries, beforeGC.RevClosingEntries),
		RevOrigClosingEntries:  saturatingUint64Delta(afterGC.RevOrigClosingEntries, beforeGC.RevOrigClosingEntries),
		RevReplyClosingEntries: saturatingUint64Delta(afterGC.RevReplyClosingEntries, beforeGC.RevReplyClosingEntries),
		RevFullClosingEntries:  saturatingUint64Delta(afterGC.RevFullClosingEntries, beforeGC.RevFullClosingEntries),
		RevReverseEntries:      saturatingUint64Delta(afterGC.RevReverseEntries, beforeGC.RevReverseEntries),
		RevAliasEntries:        saturatingUint64Delta(afterGC.RevAliasEntries, beforeGC.RevAliasEntries),
		TranslatedPortsUsed:    saturatingUint64Delta(afterGC.TranslatedPortsUsed, beforeGC.TranslatedPortsUsed),
		UDPTranslatedPortsUsed: saturatingUint64Delta(afterGC.UDPTranslatedPortsUsed, beforeGC.UDPTranslatedPortsUsed),
	}
}

func MergeSNATMapStatsPeak(current, candidate bpfnet.SNATMapStats) bpfnet.SNATMapStats {
	if candidate.TranslatedPortsUsed > current.TranslatedPortsUsed {
		return candidate
	}
	if candidate.TranslatedPortsUsed == current.TranslatedPortsUsed &&
		candidate.FwdEntries+candidate.RevEntries > current.FwdEntries+current.RevEntries {
		return candidate
	}
	return current
}

func saturatingUint64Delta(before, after uint64) uint64 {
	if after <= before {
		return 0
	}
	return after - before
}

func maxFloat64(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func ReadCPUSnapshot() (CPUSnapshot, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return CPUSnapshot{}, fmt.Errorf("open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return CPUSnapshot{}, fmt.Errorf("scan /proc/stat: %w", err)
		}
		return CPUSnapshot{}, fmt.Errorf("read /proc/stat: missing cpu line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUSnapshot{}, fmt.Errorf("read /proc/stat: malformed cpu line %q", scanner.Text())
	}

	var total uint64
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return CPUSnapshot{}, fmt.Errorf("parse /proc/stat field %q: %w", field, err)
		}
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return CPUSnapshot{total: total, idle: idle}, nil
}

func CPUUsagePercent(before, after CPUSnapshot) float64 {
	if after.total <= before.total {
		return 0
	}
	totalDelta := after.total - before.total
	idleDelta := uint64(0)
	if after.idle > before.idle {
		idleDelta = after.idle - before.idle
	}
	busyDelta := totalDelta - idleDelta
	return (float64(busyDelta) / float64(totalDelta)) * 100
}

func WriteReport(path string, report Report) error {
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal benchmark report: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func ReadReport(path string) (Report, error) {
	var report Report
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("unmarshal benchmark report: %w", err)
	}
	return report, nil
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func durationAtPercentile(sorted []time.Duration, percentile float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * percentile)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
