package natbench

import (
	"fmt"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
)

func BuildCompareReport(iptablesRuns, ebpfRuns []Report) (CompareReport, error) {
	iptables, err := AggregateReports(iptablesRuns)
	if err != nil {
		return CompareReport{}, fmt.Errorf("aggregate iptables reports: %w", err)
	}
	ebpf, err := AggregateReports(ebpfRuns)
	if err != nil {
		return CompareReport{}, fmt.Errorf("aggregate ebpf reports: %w", err)
	}

	report := CompareReport{
		Iptables: iptables,
		EBPF:     ebpf,
		Aggregation: AggregationInfo{
			IptablesRuns: len(iptablesRuns),
			EBPFRuns:     len(ebpfRuns),
			Method:       "median per path metric across repeated runs",
		},
	}

	ebpfPaths := make(map[string]PathBenchmark, len(ebpf.Paths))
	for _, path := range ebpf.Paths {
		ebpfPaths[path.Name] = path
	}
	for _, ipt := range iptables.Paths {
		ebpfPath, ok := ebpfPaths[ipt.Name]
		if !ok {
			return CompareReport{}, fmt.Errorf("missing ebpf path %q in aggregated report", ipt.Name)
		}
		iptablesReliability := summarizePathReliability(iptablesRuns, ipt.Name)
		ebpfReliability := summarizePathReliability(ebpfRuns, ebpfPath.Name)
		report.Comparison = append(report.Comparison, ComparisonPath{
			Name: ipt.Name,
			Iptables: ComparisonDatapath{
				ThroughputRPS:     ipt.Summary.ThroughputRPS,
				P50Ms:             ipt.Summary.Latency.P50Ms,
				P95Ms:             ipt.Summary.Latency.P95Ms,
				P99Ms:             ipt.Summary.Latency.P99Ms,
				HostCPUPercent:    ipt.HostCPUPercent,
				TotalRequests:     iptablesReliability.totalRequests,
				TotalSuccesses:    iptablesReliability.totalSuccesses,
				TotalFailures:     iptablesReliability.totalFailures,
				FailureRate:       iptablesReliability.failureRate(),
				RunsWithFailures:  iptablesReliability.runsWithFailures,
				MaxFailures:       iptablesReliability.maxFailures,
				FirstError:        iptablesReliability.firstError,
				Datapath:          ipt.Datapath,
				Profile:           ipt.Profile,
				ClientAfter:       ipt.ClientAfter,
				ClientPeak:        ipt.ClientPeak,
				ClientDelta:       ipt.ClientDelta,
				ClientPeakDelta:   ipt.ClientPeakDelta,
				ClientSamples:     ipt.ClientSamples,
				KernelBefore:      ipt.KernelBefore,
				KernelAfter:       ipt.KernelAfter,
				KernelDelta:       ipt.KernelDelta,
				SNATMapAfter:      ipt.SNATMapAfter,
				SNATMapDelta:      ipt.SNATMapDelta,
				SNATMapPeak:       ipt.SNATMapPeak,
				SNATMapPeakDelta:  ipt.SNATMapPeakDelta,
				SNATMapSamples:    ipt.SNATMapSamples,
				SNATMapPostGC:     ipt.SNATMapPostGC,
				SNATMapGCReleased: ipt.SNATMapGCReleased,
				SampleCount:       len(iptablesRuns),
			},
			EBPF: ComparisonDatapath{
				ThroughputRPS:     ebpfPath.Summary.ThroughputRPS,
				P50Ms:             ebpfPath.Summary.Latency.P50Ms,
				P95Ms:             ebpfPath.Summary.Latency.P95Ms,
				P99Ms:             ebpfPath.Summary.Latency.P99Ms,
				HostCPUPercent:    ebpfPath.HostCPUPercent,
				TotalRequests:     ebpfReliability.totalRequests,
				TotalSuccesses:    ebpfReliability.totalSuccesses,
				TotalFailures:     ebpfReliability.totalFailures,
				FailureRate:       ebpfReliability.failureRate(),
				RunsWithFailures:  ebpfReliability.runsWithFailures,
				MaxFailures:       ebpfReliability.maxFailures,
				FirstError:        ebpfReliability.firstError,
				Datapath:          ebpfPath.Datapath,
				Mode:              ebpfPath.Mode,
				Profile:           ebpfPath.Profile,
				ClientAfter:       ebpfPath.ClientAfter,
				ClientPeak:        ebpfPath.ClientPeak,
				ClientDelta:       ebpfPath.ClientDelta,
				ClientPeakDelta:   ebpfPath.ClientPeakDelta,
				ClientSamples:     ebpfPath.ClientSamples,
				KernelBefore:      ebpfPath.KernelBefore,
				KernelAfter:       ebpfPath.KernelAfter,
				KernelDelta:       ebpfPath.KernelDelta,
				SNATMapAfter:      ebpfPath.SNATMapAfter,
				SNATMapDelta:      ebpfPath.SNATMapDelta,
				SNATMapPeak:       ebpfPath.SNATMapPeak,
				SNATMapPeakDelta:  ebpfPath.SNATMapPeakDelta,
				SNATMapSamples:    ebpfPath.SNATMapSamples,
				SNATMapPostGC:     ebpfPath.SNATMapPostGC,
				SNATMapGCReleased: ebpfPath.SNATMapGCReleased,
				SampleCount:       len(ebpfRuns),
			},
			Delta: ComparisonDelta{
				ThroughputPct: percentDelta(ipt.Summary.ThroughputRPS, ebpfPath.Summary.ThroughputRPS),
				P95Pct:        percentDelta(ipt.Summary.Latency.P95Ms, ebpfPath.Summary.Latency.P95Ms),
				HostCPUPct:    percentDelta(ipt.HostCPUPercent, ebpfPath.HostCPUPercent),
			},
		})
	}
	return report, nil
}

type pathReliability struct {
	totalRequests    int
	totalSuccesses   int
	totalFailures    int
	runsWithFailures int
	maxFailures      int
	firstError       string
}

func (r pathReliability) failureRate() float64 {
	total := r.totalSuccesses + r.totalFailures
	if total == 0 {
		return 0
	}
	return float64(r.totalFailures) / float64(total)
}

func summarizePathReliability(reports []Report, pathName string) pathReliability {
	var out pathReliability
	for _, report := range reports {
		path, ok := findPath(report.Paths, pathName)
		if !ok {
			continue
		}
		summary := path.Summary
		out.totalRequests += summary.Requests
		out.totalSuccesses += summary.Successes
		out.totalFailures += summary.Failures
		if summary.Failures > 0 {
			out.runsWithFailures++
			if summary.Failures > out.maxFailures {
				out.maxFailures = summary.Failures
			}
			if out.firstError == "" {
				out.firstError = summary.FirstError
			}
		}
	}
	return out
}

func AggregateReports(reports []Report) (Report, error) {
	if len(reports) == 0 {
		return Report{}, fmt.Errorf("no reports provided")
	}
	if len(reports) == 1 {
		return reports[0], nil
	}

	base := reports[0]
	aggregated := Report{
		Runtime:     base.Runtime,
		NATBackend:  base.NATBackend,
		StartedAt:   earliestStartedAt(reports),
		CompletedAt: latestCompletedAt(reports),
		Startup:     aggregateStartupSummaries(startupSummaries(reports)),
	}

	for _, basePath := range base.Paths {
		samples := make([]PathBenchmark, 0, len(reports))
		for _, report := range reports {
			path, ok := findPath(report.Paths, basePath.Name)
			if !ok {
				return Report{}, fmt.Errorf("missing path %q in report %s/%s", basePath.Name, report.Runtime, report.NATBackend)
			}
			samples = append(samples, path)
		}
		aggregated.Paths = append(aggregated.Paths, aggregatePathSamples(samples))
	}
	return aggregated, nil
}

func aggregatePathSamples(samples []PathBenchmark) PathBenchmark {
	base := samples[0]
	out := base
	out.Datapath = mostCommonString(pathDatapaths(samples))
	out.Mode = mostCommonString(pathModes(samples))
	out.HostCPUPercent = medianFloat64(pathHostCPUs(samples))
	out.ObservedAt = latestObservedAt(samples)
	out.Summary = aggregateWorkloadSummaries(pathSummaries(samples))
	out.Profile = aggregateNATProfile(pathProfiles(samples))
	out.ClientBefore = aggregateClientResourceSnapshots(pathClientBefore(samples))
	out.ClientAfter = aggregateClientResourceSnapshots(pathClientAfter(samples))
	out.ClientPeak = aggregateClientResourceSnapshots(pathClientPeaks(samples))
	out.ClientDelta = aggregateClientResourceDeltas(pathClientDeltas(samples))
	out.ClientPeakDelta = aggregateClientResourceDeltas(pathClientPeakDeltas(samples))
	out.ClientSamples = medianUint64(pathClientSamples(samples))
	out.KernelBefore = aggregateKernelStats(pathKernelBefore(samples))
	out.KernelAfter = aggregateKernelStats(pathKernelAfter(samples))
	out.KernelDelta = aggregateKernelStats(pathKernelDelta(samples))
	out.SNATMapBefore = aggregateSNATMapStats(pathSNATMapBefore(samples))
	out.SNATMapAfter = aggregateSNATMapStats(pathSNATMapAfter(samples))
	out.SNATMapDelta = aggregateSNATMapStats(pathSNATMapDelta(samples))
	out.SNATMapPeak = aggregateSNATMapStats(pathSNATMapPeak(samples))
	out.SNATMapPeakDelta = aggregateSNATMapStats(pathSNATMapPeakDelta(samples))
	out.SNATMapSamples = medianUint64(pathSNATMapSamples(samples))
	out.SNATMapPostGC = aggregateSNATMapStats(pathSNATMapPostGC(samples))
	out.SNATMapGCReleased = aggregateSNATMapStats(pathSNATMapGCReleased(samples))
	out.Attachment = aggregateAttachment(pathAttachments(samples))
	if len(samples) > 1 {
		out.Notes = appendUniqueStrings(out.Notes, fmt.Sprintf("aggregated across %d runs using median per path metric", len(samples)))
	}
	return out
}

func aggregateWorkloadSummaries(samples []WorkloadSummary) WorkloadSummary {
	base := samples[0]
	failures := int(medianUint64(summaryFailures(samples)))
	firstError := ""
	if failures > 0 {
		firstError = firstNonEmpty(summaryFirstErrors(samples))
	}
	return WorkloadSummary{
		Requests:        base.Requests,
		Concurrency:     base.Concurrency,
		Successes:       int(medianUint64(summarySuccesses(samples))),
		Failures:        failures,
		FirstError:      firstError,
		DurationSeconds: medianFloat64(summaryDurations(samples)),
		ThroughputRPS:   medianFloat64(summaryThroughputs(samples)),
		Latency: LatencySummary{
			P50Ms: medianFloat64(summaryP50s(samples)),
			P95Ms: medianFloat64(summaryP95s(samples)),
			P99Ms: medianFloat64(summaryP99s(samples)),
		},
	}
}

func aggregateKernelStats(samples []bpfnet.KernelStats) bpfnet.KernelStats {
	return bpfnet.KernelStats{
		AttachSuccesses:                    medianUint64(kernelAttachSuccesses(samples)),
		ServiceHits:                        medianUint64(kernelServiceHits(samples)),
		RevNATHits:                         medianUint64(kernelRevNATHits(samples)),
		SNATHits:                           medianUint64(kernelSNATHits(samples)),
		SNATRevHits:                        medianUint64(kernelSNATRevHits(samples)),
		SNATFwdHits:                        medianUint64(kernelSNATFwdHits(samples)),
		SNATUDPSamePortHits:                medianUint64(kernelSNATUDPSamePortHits(samples)),
		SNATUDPPortRewriteHits:             medianUint64(kernelSNATUDPPortRewriteHits(samples)),
		SNATUDPChecksumHits:                medianUint64(kernelSNATUDPChecksumHits(samples)),
		SNATMappingsProgrammed:             medianUint64(kernelSNATMappingsProgrammed(samples)),
		SNATAllocCollisions:                medianUint64(kernelSNATAllocCollisions(samples)),
		SNATFallbackHits:                   medianUint64(kernelSNATFallbackHits(samples)),
		SNATAllocExhausted:                 medianUint64(kernelSNATAllocExhausted(samples)),
		SNATTCPNonSynMisses:                medianUint64(kernelSNATTCPNonSynMisses(samples)),
		SNATTCPNonSynMissFINs:              medianUint64(kernelSNATTCPNonSynMissFINs(samples)),
		SNATTCPNonSynMissRSTs:              medianUint64(kernelSNATTCPNonSynMissRSTs(samples)),
		SNATTCPNonSynMissACKs:              medianUint64(kernelSNATTCPNonSynMissACKs(samples)),
		SNATTCPNonSynMissOther:             medianUint64(kernelSNATTCPNonSynMissOther(samples)),
		SNATFullCloseReclaims:              medianUint64(kernelSNATFullCloseReclaims(samples)),
		SNATFullCloseMarks:                 medianUint64(kernelSNATFullCloseMarks(samples)),
		SNATTCPFullCloseDeletes:            medianUint64(kernelSNATTCPFullCloseDeletes(samples)),
		SNATTCPFullCloseDeletesFwd:         medianUint64(kernelSNATTCPFullCloseDeletesFwd(samples)),
		SNATTCPFullCloseDeletesRev:         medianUint64(kernelSNATTCPFullCloseDeletesRev(samples)),
		SNATTCPNonSynMissFwdLookups:        medianUint64(kernelSNATTCPNonSynMissFwdLookups(samples)),
		SNATTCPNonSynMissFwdHostMismatches: medianUint64(kernelSNATTCPNonSynMissFwdHostMismatches(samples)),
		SNATTCPReverseMisses:               medianUint64(kernelSNATTCPReverseMisses(samples)),
		SNATTCPReverseMissSynACKs:          medianUint64(kernelSNATTCPReverseMissSynACKs(samples)),
		SNATTCPReverseMissFINs:             medianUint64(kernelSNATTCPReverseMissFINs(samples)),
		SNATTCPReverseMissRSTs:             medianUint64(kernelSNATTCPReverseMissRSTs(samples)),
		SNATTCPReverseMissACKs:             medianUint64(kernelSNATTCPReverseMissACKs(samples)),
		SNATTCPReverseMissOther:            medianUint64(kernelSNATTCPReverseMissOther(samples)),
		NativeRouteSkips:                   medianUint64(kernelNativeRouteSkips(samples)),
		LocalhostConnectHits:               medianUint64(kernelLocalhostConnectHits(samples)),
		LocalhostGetpeerHits:               medianUint64(kernelLocalhostGetpeerHits(samples)),
		FallbackHits:                       medianUint64(kernelFallbackHits(samples)),
		LocalhostFallbackHits:              medianUint64(kernelLocalhostFallbackHits(samples)),
		AttachErrors:                       medianUint64(kernelAttachErrors(samples)),
	}
}

func aggregateSNATMapStats(samples []bpfnet.SNATMapStats) bpfnet.SNATMapStats {
	return bpfnet.SNATMapStats{
		FwdEntries:             medianUint64(snatMapFwdEntries(samples)),
		FwdTCPEntries:          medianUint64(snatMapFwdTCPEntries(samples)),
		FwdUDPEntries:          medianUint64(snatMapFwdUDPEntries(samples)),
		FwdICMPEntries:         medianUint64(snatMapFwdICMPEntries(samples)),
		FwdActiveEntries:       medianUint64(snatMapFwdActiveEntries(samples)),
		FwdClosingEntries:      medianUint64(snatMapFwdClosingEntries(samples)),
		FwdOrigClosingEntries:  medianUint64(snatMapFwdOrigClosingEntries(samples)),
		FwdReplyClosingEntries: medianUint64(snatMapFwdReplyClosingEntries(samples)),
		FwdFullClosingEntries:  medianUint64(snatMapFwdFullClosingEntries(samples)),
		RevEntries:             medianUint64(snatMapRevEntries(samples)),
		RevTCPEntries:          medianUint64(snatMapRevTCPEntries(samples)),
		RevUDPEntries:          medianUint64(snatMapRevUDPEntries(samples)),
		RevICMPEntries:         medianUint64(snatMapRevICMPEntries(samples)),
		RevActiveEntries:       medianUint64(snatMapRevActiveEntries(samples)),
		RevClosingEntries:      medianUint64(snatMapRevClosingEntries(samples)),
		RevOrigClosingEntries:  medianUint64(snatMapRevOrigClosingEntries(samples)),
		RevReplyClosingEntries: medianUint64(snatMapRevReplyClosingEntries(samples)),
		RevFullClosingEntries:  medianUint64(snatMapRevFullClosingEntries(samples)),
		RevReverseEntries:      medianUint64(snatMapRevReverseEntries(samples)),
		RevAliasEntries:        medianUint64(snatMapRevAliasEntries(samples)),
		TranslatedPortsUsed:    medianUint64(snatMapTranslatedPortsUsed(samples)),
		UDPTranslatedPortsUsed: medianUint64(snatMapUDPTranslatedPortsUsed(samples)),
	}
}

func aggregateNATProfile(samples []NATProfile) NATProfile {
	return NATProfile{
		SNATMappingsPerSuccess:  medianFloat64(profileMappingsPerSuccess(samples)),
		SNATForwardReuseRatio:   medianFloat64(profileForwardReuseRatios(samples)),
		SNATReverseHitRatio:     medianFloat64(profileReverseHitRatios(samples)),
		SNATFallbackRatio:       medianFloat64(profileFallbackRatios(samples)),
		SNATAllocExhaustedRatio: medianFloat64(profileAllocExhaustedRatios(samples)),
		SNATTCPNonSynMissRatio:  medianFloat64(profileTCPNonSynMissRatios(samples)),
		SNATUDPSamePortRatio:    medianFloat64(profileUDPSamePortRatios(samples)),
		SNATUDPPortRewriteRatio: medianFloat64(profileUDPPortRewriteRatios(samples)),
		SNATUDPChecksumRatio:    medianFloat64(profileUDPChecksumRatios(samples)),
	}
}

func aggregateAttachment(samples []bpfnet.AttachmentReadiness) bpfnet.AttachmentReadiness {
	return bpfnet.AttachmentReadiness{
		UplinkDevices:          appendUniqueStrings(nil, attachmentUplinkDevices(samples)...),
		LocalAddresses:         appendUniqueStrings(nil, attachmentLocalAddresses(samples)...),
		IngressTCAttached:      allTrue(attachmentIngressReady(samples)),
		EgressTCAttached:       allTrue(attachmentEgressReady(samples)),
		LocalhostLinksAttached: allTrue(attachmentLocalhostReady(samples)),
		PinnedMapsReady:        allTrue(attachmentPinnedMapsReady(samples)),
		PinnedProgramsReady:    allTrue(attachmentPinnedProgramsReady(samples)),
	}
}

func aggregateStartupSummaries(summaries []*StartupSummary) *StartupSummary {
	var base *StartupSummary
	for _, summary := range summaries {
		if summary != nil {
			base = summary
			break
		}
	}
	if base == nil {
		return nil
	}

	out := &StartupSummary{
		Runtime:    base.Runtime,
		RootfsType: base.RootfsType,
		Classes:    map[string]StartupClassSummary{},
		Phases:     map[string]StartupPhaseSummary{},
	}

	classDurationWeight := make(map[string]float64)
	bundleMaterializeWeight := 0.0
	bundleMaterializeCount := uint64(0)
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		for startClass, classSummary := range summary.Classes {
			aggregated := out.Classes[startClass]
			aggregated.OKCount += classSummary.OKCount
			aggregated.ErrorCount += classSummary.ErrorCount
			aggregated.Histogram = mergeHistogram(aggregated.Histogram, classSummary.Histogram)
			classDurationWeight[startClass] += classSummary.AverageDurationSeconds * float64(classSummary.OKCount)
			out.Classes[startClass] = aggregated
		}
		for phase, phaseSummary := range summary.Phases {
			aggregatedPhase := out.Phases[phase]
			if aggregatedPhase.Classes == nil {
				aggregatedPhase.Classes = map[string]StartupPhaseClassSummary{}
			}
			for startClass, classSummary := range phaseSummary.Classes {
				aggregatedClass := aggregatedPhase.Classes[startClass]
				aggregatedClass.Count += classSummary.Count
				aggregatedClass.ErrorCount += classSummary.ErrorCount
				aggregatedClass.Histogram = mergeHistogram(aggregatedClass.Histogram, classSummary.Histogram)
				aggregatedClass.AverageDurationSeconds += classSummary.AverageDurationSeconds * float64(classSummary.Count)
				aggregatedPhase.Classes[startClass] = aggregatedClass
			}
			out.Phases[phase] = aggregatedPhase
		}
		if summary.WaitGrace != nil {
			if out.WaitGrace == nil {
				out.WaitGrace = &RuntimeWaitGraceSummary{}
			}
			out.WaitGrace.RecoveredCount += summary.WaitGrace.RecoveredCount
			out.WaitGrace.UnavailableCount += summary.WaitGrace.UnavailableCount
		}
		if summary.Bundle != nil {
			if out.Bundle == nil {
				out.Bundle = &BundleTemplateSummary{}
			}
			out.Bundle.HitCount += summary.Bundle.HitCount
			out.Bundle.MissCount += summary.Bundle.MissCount
			out.Bundle.ErrorCount += summary.Bundle.ErrorCount
			out.Bundle.MaterializeHistogram = mergeHistogram(out.Bundle.MaterializeHistogram, summary.Bundle.MaterializeHistogram)
			count := summary.Bundle.HitCount + summary.Bundle.MissCount
			bundleMaterializeWeight += summary.Bundle.AverageMaterializeDurationSec * float64(count)
			bundleMaterializeCount += count
		}
	}

	for startClass, aggregated := range out.Classes {
		if aggregated.Histogram != nil && aggregated.Histogram.Count > 0 {
			aggregated.AverageDurationSeconds = aggregated.Histogram.SumSeconds / float64(aggregated.Histogram.Count)
			aggregated.Quantiles = histogramQuantiles(aggregated.Histogram)
		} else if aggregated.OKCount > 0 {
			aggregated.AverageDurationSeconds = classDurationWeight[startClass] / float64(aggregated.OKCount)
		}
		out.Classes[startClass] = aggregated
	}
	if len(out.Classes) == 0 {
		out.Classes = nil
	}

	for phase, aggregatedPhase := range out.Phases {
		for startClass, aggregatedClass := range aggregatedPhase.Classes {
			if aggregatedClass.Histogram != nil && aggregatedClass.Histogram.Count > 0 {
				aggregatedClass.AverageDurationSeconds = aggregatedClass.Histogram.SumSeconds / float64(aggregatedClass.Histogram.Count)
				aggregatedClass.Quantiles = histogramQuantiles(aggregatedClass.Histogram)
			} else if aggregatedClass.Count > 0 {
				aggregatedClass.AverageDurationSeconds = aggregatedClass.AverageDurationSeconds / float64(aggregatedClass.Count)
			}
			aggregatedPhase.Classes[startClass] = aggregatedClass
		}
		out.Phases[phase] = aggregatedPhase
	}
	if len(out.Phases) == 0 {
		out.Phases = nil
	}
	if out.Bundle != nil {
		if out.Bundle.MaterializeHistogram != nil && out.Bundle.MaterializeHistogram.Count > 0 {
			out.Bundle.AverageMaterializeDurationSec = out.Bundle.MaterializeHistogram.SumSeconds / float64(out.Bundle.MaterializeHistogram.Count)
			out.Bundle.MaterializeQuantiles = histogramQuantiles(out.Bundle.MaterializeHistogram)
		} else if bundleMaterializeCount > 0 {
			out.Bundle.AverageMaterializeDurationSec = bundleMaterializeWeight / float64(bundleMaterializeCount)
		}
		if out.Bundle.HitCount == 0 && out.Bundle.MissCount == 0 && out.Bundle.ErrorCount == 0 && out.Bundle.AverageMaterializeDurationSec == 0 {
			out.Bundle = nil
		}
	}
	if out.WaitGrace != nil && out.WaitGrace.RecoveredCount == 0 && out.WaitGrace.UnavailableCount == 0 {
		out.WaitGrace = nil
	}
	finalizeStartupSummary(out)

	return out
}

func findPath(paths []PathBenchmark, name string) (PathBenchmark, bool) {
	for _, path := range paths {
		if path.Name == name {
			return path, true
		}
	}
	return PathBenchmark{}, false
}

func earliestStartedAt(reports []Report) time.Time {
	earliest := reports[0].StartedAt
	for _, report := range reports[1:] {
		if report.StartedAt.Before(earliest) {
			earliest = report.StartedAt
		}
	}
	return earliest
}

func latestCompletedAt(reports []Report) time.Time {
	latest := reports[0].CompletedAt
	for _, report := range reports[1:] {
		if report.CompletedAt.After(latest) {
			latest = report.CompletedAt
		}
	}
	return latest
}

func latestObservedAt(samples []PathBenchmark) time.Time {
	latest := samples[0].ObservedAt
	for _, sample := range samples[1:] {
		if sample.ObservedAt.After(latest) {
			latest = sample.ObservedAt
		}
	}
	return latest
}

func startupSummaries(reports []Report) []*StartupSummary {
	out := make([]*StartupSummary, 0, len(reports))
	for _, report := range reports {
		out = append(out, report.Startup)
	}
	return out
}
