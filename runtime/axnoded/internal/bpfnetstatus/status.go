package bpfnetstatus

import (
	"fmt"

	"github.com/cofy-x/axern/network/bpfnet"
)

func Load(pinPath string) (bpfnet.Status, error) {
	ctrl := bpfnet.NewController(bpfnet.Config{PinPath: pinPath})
	return ctrl.Status()
}

func KernelDelta(before, after bpfnet.KernelStats) bpfnet.KernelStats {
	return bpfnet.KernelStats{
		AttachSuccesses:                    saturatingDelta(after.AttachSuccesses, before.AttachSuccesses),
		SNATHits:                           saturatingDelta(after.SNATHits, before.SNATHits),
		SNATRevHits:                        saturatingDelta(after.SNATRevHits, before.SNATRevHits),
		SNATFwdHits:                        saturatingDelta(after.SNATFwdHits, before.SNATFwdHits),
		SNATUDPSamePortHits:                saturatingDelta(after.SNATUDPSamePortHits, before.SNATUDPSamePortHits),
		SNATUDPPortRewriteHits:             saturatingDelta(after.SNATUDPPortRewriteHits, before.SNATUDPPortRewriteHits),
		SNATUDPChecksumHits:                saturatingDelta(after.SNATUDPChecksumHits, before.SNATUDPChecksumHits),
		SNATMappingsProgrammed:             saturatingDelta(after.SNATMappingsProgrammed, before.SNATMappingsProgrammed),
		SNATAllocCollisions:                saturatingDelta(after.SNATAllocCollisions, before.SNATAllocCollisions),
		SNATFallbackHits:                   saturatingDelta(after.SNATFallbackHits, before.SNATFallbackHits),
		SNATAllocExhausted:                 saturatingDelta(after.SNATAllocExhausted, before.SNATAllocExhausted),
		SNATTCPNonSynMisses:                saturatingDelta(after.SNATTCPNonSynMisses, before.SNATTCPNonSynMisses),
		SNATTCPNonSynMissFINs:              saturatingDelta(after.SNATTCPNonSynMissFINs, before.SNATTCPNonSynMissFINs),
		SNATTCPNonSynMissRSTs:              saturatingDelta(after.SNATTCPNonSynMissRSTs, before.SNATTCPNonSynMissRSTs),
		SNATTCPNonSynMissACKs:              saturatingDelta(after.SNATTCPNonSynMissACKs, before.SNATTCPNonSynMissACKs),
		SNATTCPNonSynMissOther:             saturatingDelta(after.SNATTCPNonSynMissOther, before.SNATTCPNonSynMissOther),
		SNATFullCloseReclaims:              saturatingDelta(after.SNATFullCloseReclaims, before.SNATFullCloseReclaims),
		SNATFullCloseMarks:                 saturatingDelta(after.SNATFullCloseMarks, before.SNATFullCloseMarks),
		SNATTCPFullCloseDeletes:            saturatingDelta(after.SNATTCPFullCloseDeletes, before.SNATTCPFullCloseDeletes),
		SNATTCPFullCloseDeletesFwd:         saturatingDelta(after.SNATTCPFullCloseDeletesFwd, before.SNATTCPFullCloseDeletesFwd),
		SNATTCPFullCloseDeletesRev:         saturatingDelta(after.SNATTCPFullCloseDeletesRev, before.SNATTCPFullCloseDeletesRev),
		SNATTCPNonSynMissFwdLookups:        saturatingDelta(after.SNATTCPNonSynMissFwdLookups, before.SNATTCPNonSynMissFwdLookups),
		SNATTCPNonSynMissFwdHostMismatches: saturatingDelta(after.SNATTCPNonSynMissFwdHostMismatches, before.SNATTCPNonSynMissFwdHostMismatches),
		SNATTCPReverseMisses:               saturatingDelta(after.SNATTCPReverseMisses, before.SNATTCPReverseMisses),
		SNATTCPReverseMissSynACKs:          saturatingDelta(after.SNATTCPReverseMissSynACKs, before.SNATTCPReverseMissSynACKs),
		SNATTCPReverseMissFINs:             saturatingDelta(after.SNATTCPReverseMissFINs, before.SNATTCPReverseMissFINs),
		SNATTCPReverseMissRSTs:             saturatingDelta(after.SNATTCPReverseMissRSTs, before.SNATTCPReverseMissRSTs),
		SNATTCPReverseMissACKs:             saturatingDelta(after.SNATTCPReverseMissACKs, before.SNATTCPReverseMissACKs),
		SNATTCPReverseMissOther:            saturatingDelta(after.SNATTCPReverseMissOther, before.SNATTCPReverseMissOther),
		NativeRouteSkips:                   saturatingDelta(after.NativeRouteSkips, before.NativeRouteSkips),
		AttachErrors:                       saturatingDelta(after.AttachErrors, before.AttachErrors),
	}
}

func RequireTCReady(status bpfnet.Status) error {
	state := status.State
	if !state.TCReady {
		return fmt.Errorf("expected tc dataplane to be ready: %#v", state)
	}
	if !status.Attachment.IngressTCAttached || !status.Attachment.EgressTCAttached {
		return fmt.Errorf("expected tc ingress/egress filters to be attached: %#v", status.Attachment)
	}
	if !status.Attachment.PinnedMapsReady {
		return fmt.Errorf("expected pinned maps to be ready: %#v", status.Attachment)
	}
	if !status.Attachment.PinnedProgramsReady {
		return fmt.Errorf("expected pinned programs to be ready: %#v", status.Attachment)
	}
	return nil
}

func saturatingDelta(after, before uint64) uint64 {
	if after <= before {
		return 0
	}
	return after - before
}
