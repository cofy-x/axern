package bpfnet

import internaldataplane "github.com/cofy-x/axern/network/bpfnet/internal/dataplane"

var (
	collectKernelStats         = defaultCollectKernelStats
	collectSNATMapStats        = defaultCollectSNATMapStats
	collectAttachmentReadiness = defaultCollectAttachmentReadiness
)

func defaultCollectKernelStats(cfg Config) KernelStats {
	stats := internaldataplane.CollectKernelStats(toInternalConfig(cfg))
	return KernelStats{
		AttachSuccesses:                    stats.AttachSuccesses,
		ServiceHits:                        stats.ServiceHits,
		RevNATHits:                         stats.RevNATHits,
		SNATHits:                           stats.SNATHits,
		SNATRevHits:                        stats.SNATRevHits,
		SNATFwdHits:                        stats.SNATFwdHits,
		SNATUDPSamePortHits:                stats.SNATUDPSamePortHits,
		SNATUDPPortRewriteHits:             stats.SNATUDPPortRewriteHits,
		SNATUDPChecksumHits:                stats.SNATUDPChecksumHits,
		SNATMappingsProgrammed:             stats.SNATMappingsProgrammed,
		SNATAllocCollisions:                stats.SNATAllocCollisions,
		SNATFallbackHits:                   stats.SNATFallbackHits,
		SNATAllocExhausted:                 stats.SNATAllocExhausted,
		SNATTCPNonSynMisses:                stats.SNATTCPNonSynMisses,
		SNATTCPNonSynMissFINs:              stats.SNATTCPNonSynMissFINs,
		SNATTCPNonSynMissRSTs:              stats.SNATTCPNonSynMissRSTs,
		SNATTCPNonSynMissACKs:              stats.SNATTCPNonSynMissACKs,
		SNATTCPNonSynMissOther:             stats.SNATTCPNonSynMissOther,
		SNATFullCloseReclaims:              stats.SNATFullCloseReclaims,
		SNATFullCloseMarks:                 stats.SNATFullCloseMarks,
		SNATTCPFullCloseDeletes:            stats.SNATTCPFullCloseDeletes,
		SNATTCPFullCloseDeletesFwd:         stats.SNATTCPFullCloseDeletesFwd,
		SNATTCPFullCloseDeletesRev:         stats.SNATTCPFullCloseDeletesRev,
		SNATTCPNonSynMissFwdLookups:        stats.SNATTCPNonSynMissFwdLookups,
		SNATTCPNonSynMissFwdHostMismatches: stats.SNATTCPNonSynMissFwdHostMismatches,
		SNATTCPReverseMisses:               stats.SNATTCPReverseMisses,
		SNATTCPReverseMissSynACKs:          stats.SNATTCPReverseMissSynACKs,
		SNATTCPReverseMissFINs:             stats.SNATTCPReverseMissFINs,
		SNATTCPReverseMissRSTs:             stats.SNATTCPReverseMissRSTs,
		SNATTCPReverseMissACKs:             stats.SNATTCPReverseMissACKs,
		SNATTCPReverseMissOther:            stats.SNATTCPReverseMissOther,
		NativeRouteSkips:                   stats.NativeRouteSkips,
		LocalhostConnectHits:               stats.LocalhostConnectHits,
		LocalhostGetpeerHits:               stats.LocalhostGetpeerHits,
		FallbackHits:                       stats.FallbackHits,
		LocalhostFallbackHits:              stats.LocalhostFallbackHits,
		AttachErrors:                       stats.AttachErrors,
	}
}

func defaultCollectSNATMapStats(cfg Config) SNATMapStats {
	stats := internaldataplane.CollectSNATMapStats(toInternalConfig(cfg))
	return SNATMapStats{
		FwdEntries:             stats.FwdEntries,
		FwdTCPEntries:          stats.FwdTCPEntries,
		FwdUDPEntries:          stats.FwdUDPEntries,
		FwdICMPEntries:         stats.FwdICMPEntries,
		FwdActiveEntries:       stats.FwdActiveEntries,
		FwdClosingEntries:      stats.FwdClosingEntries,
		FwdOrigClosingEntries:  stats.FwdOrigClosingEntries,
		FwdReplyClosingEntries: stats.FwdReplyClosingEntries,
		FwdFullClosingEntries:  stats.FwdFullClosingEntries,
		RevEntries:             stats.RevEntries,
		RevTCPEntries:          stats.RevTCPEntries,
		RevUDPEntries:          stats.RevUDPEntries,
		RevICMPEntries:         stats.RevICMPEntries,
		RevActiveEntries:       stats.RevActiveEntries,
		RevClosingEntries:      stats.RevClosingEntries,
		RevOrigClosingEntries:  stats.RevOrigClosingEntries,
		RevReplyClosingEntries: stats.RevReplyClosingEntries,
		RevFullClosingEntries:  stats.RevFullClosingEntries,
		RevReverseEntries:      stats.RevReverseEntries,
		RevAliasEntries:        stats.RevAliasEntries,
		TranslatedPortsUsed:    stats.TranslatedPortsUsed,
		UDPTranslatedPortsUsed: stats.UDPTranslatedPortsUsed,
	}
}

func defaultCollectAttachmentReadiness(cfg Config, state DataplaneState) AttachmentReadiness {
	attachment := internaldataplane.CollectAttachmentReadiness(
		toInternalConfig(cfg),
		state.UplinkDevices,
		state.LocalAddresses,
		state.LocalOutCompat,
	)
	return AttachmentReadiness{
		UplinkDevices:          append([]string(nil), attachment.UplinkDevices...),
		LocalAddresses:         append([]string(nil), attachment.LocalAddresses...),
		IngressTCAttached:      attachment.IngressTCAttached,
		EgressTCAttached:       attachment.EgressTCAttached,
		LocalhostLinksAttached: attachment.LocalhostLinksAttached,
		PinnedMapsReady:        attachment.PinnedMapsReady,
		PinnedProgramsReady:    attachment.PinnedProgramsReady,
	}
}
