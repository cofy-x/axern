package bpfnetstatus

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/network/bpfnet"
)

func Load(pinPath string) (bpfnet.Status, error) {
	ctrl := bpfnet.NewController(bpfnet.Config{PinPath: pinPath})
	return ctrl.Status()
}

func FindService(status bpfnet.Status, protocol string, hostPort uint16) (bpfnet.Service, error) {
	for _, service := range status.Services {
		if strings.EqualFold(service.Protocol, protocol) && service.HostPort == hostPort {
			return service, nil
		}
	}
	return bpfnet.Service{}, fmt.Errorf("bpfnet service map missing %s host port %d", strings.ToLower(protocol), hostPort)
}

func KernelDelta(before, after bpfnet.KernelStats) bpfnet.KernelStats {
	return bpfnet.KernelStats{
		AttachSuccesses:                    saturatingDelta(after.AttachSuccesses, before.AttachSuccesses),
		ServiceHits:                        saturatingDelta(after.ServiceHits, before.ServiceHits),
		RevNATHits:                         saturatingDelta(after.RevNATHits, before.RevNATHits),
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
		LocalhostConnectHits:               saturatingDelta(after.LocalhostConnectHits, before.LocalhostConnectHits),
		LocalhostGetpeerHits:               saturatingDelta(after.LocalhostGetpeerHits, before.LocalhostGetpeerHits),
		FallbackHits:                       saturatingDelta(after.FallbackHits, before.FallbackHits),
		LocalhostFallbackHits:              saturatingDelta(after.LocalhostFallbackHits, before.LocalhostFallbackHits),
		AttachErrors:                       saturatingDelta(after.AttachErrors, before.AttachErrors),
	}
}

func RequireTCReady(status bpfnet.Status) error {
	state := status.State
	if !state.TCReady {
		return fmt.Errorf("expected tc dataplane to be ready: %#v", state)
	}
	if state.FullFallback {
		return fmt.Errorf("expected tc dataplane to avoid full fallback: %#v", state)
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

func RequireLocalhostTCPReady(status bpfnet.Status) error {
	if err := RequireTCReady(status); err != nil {
		return err
	}
	state := status.State
	if !state.LocalhostTCPDNAT || !state.LocalhostPathReady || state.LocalhostCompat {
		return fmt.Errorf("expected localhost tcp path to be active without compat fallback: %#v", state)
	}
	if !status.Attachment.LocalhostLinksAttached {
		return fmt.Errorf("expected localhost cgroup links to be attached: %#v", status.Attachment)
	}
	return nil
}

func RequireIngressUDPReady(status bpfnet.Status) error {
	if err := RequireTCReady(status); err != nil {
		return err
	}
	if !status.State.IngressUDPDNAT {
		return fmt.Errorf("expected ingress udp dnat to be enabled: %#v", status.State)
	}
	return nil
}

func saturatingDelta(after, before uint64) uint64 {
	if after <= before {
		return 0
	}
	return after - before
}
