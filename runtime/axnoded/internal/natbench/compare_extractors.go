package natbench

import "github.com/cofy-x/axern/network/bpfnet"

func pathDatapaths(samples []PathBenchmark) []string {
	values := make([]string, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Datapath)
	}
	return values
}

func pathModes(samples []PathBenchmark) []string {
	values := make([]string, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Mode)
	}
	return values
}

func pathHostCPUs(samples []PathBenchmark) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.HostCPUPercent)
	}
	return values
}

func pathClientBefore(samples []PathBenchmark) []ClientResourceSnapshot {
	values := make([]ClientResourceSnapshot, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientBefore)
	}
	return values
}

func pathClientAfter(samples []PathBenchmark) []ClientResourceSnapshot {
	values := make([]ClientResourceSnapshot, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientAfter)
	}
	return values
}

func pathClientPeaks(samples []PathBenchmark) []ClientResourceSnapshot {
	values := make([]ClientResourceSnapshot, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientPeak)
	}
	return values
}

func pathClientDeltas(samples []PathBenchmark) []ClientResourceDelta {
	values := make([]ClientResourceDelta, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientDelta)
	}
	return values
}

func pathClientPeakDeltas(samples []PathBenchmark) []ClientResourceDelta {
	values := make([]ClientResourceDelta, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientPeakDelta)
	}
	return values
}

func pathClientSamples(samples []PathBenchmark) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ClientSamples)
	}
	return values
}

func pathSummaries(samples []PathBenchmark) []WorkloadSummary {
	values := make([]WorkloadSummary, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Summary)
	}
	return values
}

func pathKernelBefore(samples []PathBenchmark) []bpfnet.KernelStats {
	values := make([]bpfnet.KernelStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.KernelBefore)
	}
	return values
}

func pathKernelAfter(samples []PathBenchmark) []bpfnet.KernelStats {
	values := make([]bpfnet.KernelStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.KernelAfter)
	}
	return values
}

func pathKernelDelta(samples []PathBenchmark) []bpfnet.KernelStats {
	values := make([]bpfnet.KernelStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.KernelDelta)
	}
	return values
}

func pathSNATMapBefore(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapBefore)
	}
	return values
}

func pathSNATMapAfter(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapAfter)
	}
	return values
}

func pathSNATMapDelta(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapDelta)
	}
	return values
}

func pathSNATMapPeak(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapPeak)
	}
	return values
}

func pathSNATMapPeakDelta(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapPeakDelta)
	}
	return values
}

func pathSNATMapSamples(samples []PathBenchmark) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapSamples)
	}
	return values
}

func pathSNATMapPostGC(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapPostGC)
	}
	return values
}

func pathSNATMapGCReleased(samples []PathBenchmark) []bpfnet.SNATMapStats {
	values := make([]bpfnet.SNATMapStats, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMapGCReleased)
	}
	return values
}

func pathAttachments(samples []PathBenchmark) []bpfnet.AttachmentReadiness {
	values := make([]bpfnet.AttachmentReadiness, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Attachment)
	}
	return values
}

func pathProfiles(samples []PathBenchmark) []NATProfile {
	values := make([]NATProfile, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Profile)
	}
	return values
}

func summarySuccesses(samples []WorkloadSummary) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, uint64(sample.Successes))
	}
	return values
}

func summaryFailures(samples []WorkloadSummary) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, uint64(sample.Failures))
	}
	return values
}

func summaryFirstErrors(samples []WorkloadSummary) []string {
	values := make([]string, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FirstError)
	}
	return values
}

func summaryDurations(samples []WorkloadSummary) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.DurationSeconds)
	}
	return values
}

func summaryThroughputs(samples []WorkloadSummary) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ThroughputRPS)
	}
	return values
}

func summaryP50s(samples []WorkloadSummary) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Latency.P50Ms)
	}
	return values
}

func summaryP95s(samples []WorkloadSummary) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Latency.P95Ms)
	}
	return values
}

func summaryP99s(samples []WorkloadSummary) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Latency.P99Ms)
	}
	return values
}

func kernelAttachSuccesses(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.AttachSuccesses)
	}
	return values
}

func kernelServiceHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.ServiceHits)
	}
	return values
}

func kernelRevNATHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevNATHits)
	}
	return values
}

func kernelSNATHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATHits)
	}
	return values
}

func kernelSNATRevHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATRevHits)
	}
	return values
}

func kernelSNATFwdHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATFwdHits)
	}
	return values
}

func kernelSNATUDPSamePortHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPSamePortHits)
	}
	return values
}

func kernelSNATUDPPortRewriteHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPPortRewriteHits)
	}
	return values
}

func kernelSNATUDPChecksumHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPChecksumHits)
	}
	return values
}

func kernelSNATMappingsProgrammed(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMappingsProgrammed)
	}
	return values
}

func kernelSNATAllocCollisions(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATAllocCollisions)
	}
	return values
}

func kernelSNATFallbackHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATFallbackHits)
	}
	return values
}

func kernelSNATAllocExhausted(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATAllocExhausted)
	}
	return values
}

func kernelSNATTCPNonSynMisses(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMisses)
	}
	return values
}

func kernelSNATTCPNonSynMissFINs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissFINs)
	}
	return values
}

func kernelSNATTCPNonSynMissRSTs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissRSTs)
	}
	return values
}

func kernelSNATTCPNonSynMissACKs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissACKs)
	}
	return values
}

func kernelSNATTCPNonSynMissOther(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissOther)
	}
	return values
}

func kernelSNATFullCloseReclaims(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATFullCloseReclaims)
	}
	return values
}

func kernelSNATFullCloseMarks(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATFullCloseMarks)
	}
	return values
}

func kernelSNATTCPFullCloseDeletes(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPFullCloseDeletes)
	}
	return values
}

func kernelSNATTCPFullCloseDeletesFwd(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPFullCloseDeletesFwd)
	}
	return values
}

func kernelSNATTCPFullCloseDeletesRev(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPFullCloseDeletesRev)
	}
	return values
}

func kernelSNATTCPNonSynMissFwdLookups(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissFwdLookups)
	}
	return values
}

func kernelSNATTCPNonSynMissFwdHostMismatches(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissFwdHostMismatches)
	}
	return values
}

func kernelSNATTCPReverseMisses(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMisses)
	}
	return values
}

func kernelSNATTCPReverseMissSynACKs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMissSynACKs)
	}
	return values
}

func kernelSNATTCPReverseMissFINs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMissFINs)
	}
	return values
}

func kernelSNATTCPReverseMissRSTs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMissRSTs)
	}
	return values
}

func kernelSNATTCPReverseMissACKs(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMissACKs)
	}
	return values
}

func kernelSNATTCPReverseMissOther(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPReverseMissOther)
	}
	return values
}

func kernelNativeRouteSkips(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.NativeRouteSkips)
	}
	return values
}

func kernelLocalhostConnectHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.LocalhostConnectHits)
	}
	return values
}

func kernelLocalhostGetpeerHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.LocalhostGetpeerHits)
	}
	return values
}

func kernelFallbackHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FallbackHits)
	}
	return values
}

func kernelLocalhostFallbackHits(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.LocalhostFallbackHits)
	}
	return values
}

func kernelAttachErrors(samples []bpfnet.KernelStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.AttachErrors)
	}
	return values
}

func snatMapFwdEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdEntries)
	}
	return values
}

func snatMapFwdTCPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdTCPEntries)
	}
	return values
}

func snatMapFwdUDPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdUDPEntries)
	}
	return values
}

func snatMapFwdICMPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdICMPEntries)
	}
	return values
}

func snatMapFwdActiveEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdActiveEntries)
	}
	return values
}

func snatMapFwdClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdClosingEntries)
	}
	return values
}

func snatMapFwdOrigClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdOrigClosingEntries)
	}
	return values
}

func snatMapFwdReplyClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdReplyClosingEntries)
	}
	return values
}

func snatMapFwdFullClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.FwdFullClosingEntries)
	}
	return values
}

func snatMapRevEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevEntries)
	}
	return values
}

func snatMapRevTCPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevTCPEntries)
	}
	return values
}

func snatMapRevUDPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevUDPEntries)
	}
	return values
}

func snatMapRevICMPEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevICMPEntries)
	}
	return values
}

func snatMapRevActiveEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevActiveEntries)
	}
	return values
}

func snatMapRevClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevClosingEntries)
	}
	return values
}

func snatMapRevOrigClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevOrigClosingEntries)
	}
	return values
}

func snatMapRevReplyClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevReplyClosingEntries)
	}
	return values
}

func snatMapRevFullClosingEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevFullClosingEntries)
	}
	return values
}

func snatMapRevReverseEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevReverseEntries)
	}
	return values
}

func snatMapRevAliasEntries(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.RevAliasEntries)
	}
	return values
}

func snatMapTranslatedPortsUsed(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.TranslatedPortsUsed)
	}
	return values
}

func snatMapUDPTranslatedPortsUsed(samples []bpfnet.SNATMapStats) []uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.UDPTranslatedPortsUsed)
	}
	return values
}

func profileMappingsPerSuccess(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATMappingsPerSuccess)
	}
	return values
}

func profileForwardReuseRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATForwardReuseRatio)
	}
	return values
}

func profileReverseHitRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATReverseHitRatio)
	}
	return values
}

func profileFallbackRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATFallbackRatio)
	}
	return values
}

func profileAllocExhaustedRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATAllocExhaustedRatio)
	}
	return values
}

func profileTCPNonSynMissRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATTCPNonSynMissRatio)
	}
	return values
}

func profileUDPSamePortRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPSamePortRatio)
	}
	return values
}

func profileUDPPortRewriteRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPPortRewriteRatio)
	}
	return values
}

func profileUDPChecksumRatios(samples []NATProfile) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.SNATUDPChecksumRatio)
	}
	return values
}

func attachmentUplinkDevices(samples []bpfnet.AttachmentReadiness) []string {
	values := make([]string, 0)
	for _, sample := range samples {
		values = append(values, sample.UplinkDevices...)
	}
	return values
}

func attachmentLocalAddresses(samples []bpfnet.AttachmentReadiness) []string {
	values := make([]string, 0)
	for _, sample := range samples {
		values = append(values, sample.LocalAddresses...)
	}
	return values
}

func attachmentIngressReady(samples []bpfnet.AttachmentReadiness) []bool {
	values := make([]bool, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.IngressTCAttached)
	}
	return values
}

func attachmentEgressReady(samples []bpfnet.AttachmentReadiness) []bool {
	values := make([]bool, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.EgressTCAttached)
	}
	return values
}

func attachmentLocalhostReady(samples []bpfnet.AttachmentReadiness) []bool {
	values := make([]bool, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.LocalhostLinksAttached)
	}
	return values
}

func attachmentPinnedMapsReady(samples []bpfnet.AttachmentReadiness) []bool {
	values := make([]bool, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.PinnedMapsReady)
	}
	return values
}

func attachmentPinnedProgramsReady(samples []bpfnet.AttachmentReadiness) []bool {
	values := make([]bool, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.PinnedProgramsReady)
	}
	return values
}
