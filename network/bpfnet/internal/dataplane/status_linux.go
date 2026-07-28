//go:build linux

package dataplane

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func CollectKernelStats(cfg Config) KernelStats {
	statsMap, err := ebpf.LoadPinnedMap(filepath.Join(cfg.PinPath, statsMapName), nil)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return KernelStats{}
		}
		return KernelStats{}
	}
	defer statsMap.Close()

	return KernelStats{
		AttachSuccesses:                    lookupKernelStat(statsMap, KernelStatAttachSuccess),
		ServiceHits:                        lookupKernelStat(statsMap, KernelStatServiceHit),
		RevNATHits:                         lookupKernelStat(statsMap, KernelStatRevNATHit),
		SNATHits:                           lookupKernelStat(statsMap, KernelStatSNATHit),
		SNATRevHits:                        lookupKernelStat(statsMap, KernelStatSNATRevHit),
		SNATFwdHits:                        lookupKernelStat(statsMap, KernelStatSNATFwdHit),
		SNATUDPSamePortHits:                lookupKernelStat(statsMap, KernelStatSNATUDPSamePortHit),
		SNATUDPPortRewriteHits:             lookupKernelStat(statsMap, KernelStatSNATUDPPortRewriteHit),
		SNATUDPChecksumHits:                lookupKernelStat(statsMap, KernelStatSNATUDPChecksumPresentHit),
		SNATMappingsProgrammed:             lookupKernelStat(statsMap, KernelStatSNATMappingProgrammed),
		SNATAllocCollisions:                lookupKernelStat(statsMap, KernelStatSNATAllocCollision),
		SNATFallbackHits:                   lookupKernelStat(statsMap, KernelStatSNATFallbackHit),
		SNATAllocExhausted:                 lookupKernelStat(statsMap, KernelStatSNATAllocExhausted),
		SNATTCPNonSynMisses:                lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMiss),
		SNATTCPNonSynMissFINs:              lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissFIN),
		SNATTCPNonSynMissRSTs:              lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissRST),
		SNATTCPNonSynMissACKs:              lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissACK),
		SNATTCPNonSynMissOther:             lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissOther),
		SNATFullCloseReclaims:              lookupKernelStat(statsMap, KernelStatSNATFullCloseReclaim),
		SNATFullCloseMarks:                 lookupKernelStat(statsMap, KernelStatSNATFullCloseMark),
		SNATTCPFullCloseDeletes:            lookupKernelStat(statsMap, KernelStatSNATTCPFullCloseDelete),
		SNATTCPFullCloseDeletesFwd:         lookupKernelStat(statsMap, KernelStatSNATTCPFullCloseDeleteFwd),
		SNATTCPFullCloseDeletesRev:         lookupKernelStat(statsMap, KernelStatSNATTCPFullCloseDeleteRev),
		SNATTCPNonSynMissFwdLookups:        lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissFwdLookup),
		SNATTCPNonSynMissFwdHostMismatches: lookupKernelStat(statsMap, KernelStatSNATTCPNonSynMissFwdHostMismatch),
		SNATTCPReverseMisses:               lookupKernelStat(statsMap, KernelStatSNATTCPRevMiss),
		SNATTCPReverseMissSynACKs:          lookupKernelStat(statsMap, KernelStatSNATTCPRevMissSynACK),
		SNATTCPReverseMissFINs:             lookupKernelStat(statsMap, KernelStatSNATTCPRevMissFIN),
		SNATTCPReverseMissRSTs:             lookupKernelStat(statsMap, KernelStatSNATTCPRevMissRST),
		SNATTCPReverseMissACKs:             lookupKernelStat(statsMap, KernelStatSNATTCPRevMissACK),
		SNATTCPReverseMissOther:            lookupKernelStat(statsMap, KernelStatSNATTCPRevMissOther),
		NativeRouteSkips:                   lookupKernelStat(statsMap, KernelStatNativeRouteSkip),
		LocalhostConnectHits:               lookupKernelStat(statsMap, KernelStatLocalhostConnectHit),
		LocalhostGetpeerHits:               lookupKernelStat(statsMap, KernelStatLocalhostGetPeerHit),
		FallbackHits:                       lookupKernelStat(statsMap, KernelStatFallbackHit),
		LocalhostFallbackHits:              lookupKernelStat(statsMap, KernelStatLocalhostFallbackHit),
		AttachErrors:                       lookupKernelStat(statsMap, KernelStatAttachError),
	}
}

func CollectSNATMapStats(cfg Config) SNATMapStats {
	var stats SNATMapStats
	translatedPorts := make(map[uint16]struct{})
	translatedUDPPorts := make(map[uint16]struct{})

	if fwdMap, err := ebpf.LoadPinnedMap(filepath.Join(cfg.PinPath, snatFwdMapName), nil); err == nil {
		defer fwdMap.Close()
		var key tcprog.DataplaneSnatFwdKey
		var value tcprog.DataplaneSnatFwdValue
		iterator := fwdMap.Iterate()
		for iterator.Next(&key, &value) {
			stats.FwdEntries++
			recordSNATProtocol(key.Proto, &stats.FwdTCPEntries, &stats.FwdUDPEntries, &stats.FwdICMPEntries)
			recordSNATFlowState(value.State, &stats.FwdActiveEntries, &stats.FwdClosingEntries, &stats.FwdOrigClosingEntries, &stats.FwdReplyClosingEntries, &stats.FwdFullClosingEntries)
			if value.TranslatedSrc != 0 {
				translatedPorts[value.TranslatedSrc] = struct{}{}
				if key.Proto == unix.IPPROTO_UDP {
					translatedUDPPorts[value.TranslatedSrc] = struct{}{}
				}
			}
		}
	}

	if revMap, err := ebpf.LoadPinnedMap(filepath.Join(cfg.PinPath, snatRevMapName), nil); err == nil {
		defer revMap.Close()
		var key tcprog.DataplaneSnatRevKey
		var value tcprog.DataplaneSnatRevValue
		iterator := revMap.Iterate()
		for iterator.Next(&key, &value) {
			stats.RevEntries++
			recordSNATProtocol(key.Proto, &stats.RevTCPEntries, &stats.RevUDPEntries, &stats.RevICMPEntries)
			recordSNATFlowState(value.State, &stats.RevActiveEntries, &stats.RevClosingEntries, &stats.RevOrigClosingEntries, &stats.RevReplyClosingEntries, &stats.RevFullClosingEntries)
			switch key.Flags {
			case snatEntryReverse:
				stats.RevReverseEntries++
				if key.DstPort != 0 {
					translatedPorts[key.DstPort] = struct{}{}
					if key.Proto == unix.IPPROTO_UDP {
						translatedUDPPorts[key.DstPort] = struct{}{}
					}
				}
			case snatEntryAlias:
				stats.RevAliasEntries++
				if value.TranslatedSrc != 0 {
					translatedPorts[value.TranslatedSrc] = struct{}{}
					if key.Proto == unix.IPPROTO_UDP {
						translatedUDPPorts[value.TranslatedSrc] = struct{}{}
					}
				}
			}
		}
	}

	stats.TranslatedPortsUsed = uint64(len(translatedPorts))
	stats.UDPTranslatedPortsUsed = uint64(len(translatedUDPPorts))
	return stats
}

func recordSNATProtocol(proto uint8, tcp, udp, icmp *uint64) {
	switch proto {
	case unix.IPPROTO_TCP:
		(*tcp)++
	case unix.IPPROTO_UDP:
		(*udp)++
	case unix.IPPROTO_ICMP:
		(*icmp)++
	}
}

func recordSNATFlowState(state uint8, active, closing, origClosing, replyClosing, fullClosing *uint64) {
	switch state {
	case snatFlowActive:
		(*active)++
	case snatFlowOrigClosing:
		(*closing)++
		(*origClosing)++
	case snatFlowReplyClosing:
		(*closing)++
		(*replyClosing)++
	case snatFlowClosing:
		(*closing)++
		(*fullClosing)++
	default:
		if snatFlowIsClosing(state) {
			(*closing)++
		} else {
			(*active)++
		}
	}
}

func CollectAttachmentReadiness(cfg Config, uplinks []string, localAddresses []string, localOutCompat bool) AttachmentReadiness {
	attachment := AttachmentReadiness{
		UplinkDevices:       append([]string(nil), uplinks...),
		LocalAddresses:      append([]string(nil), localAddresses...),
		PinnedMapsReady:     pinnedMapsReady(cfg.PinPath),
		PinnedProgramsReady: pinnedProgramsReady(cfg.PinPath),
	}

	if len(uplinks) > 0 {
		if data, err := collectInterfaceData(uplinks); err == nil {
			attachment.LocalAddresses = data.localAddresses
		}
		attachment.IngressTCAttached = tcFiltersAttached(uplinks, netlink.HANDLE_MIN_INGRESS)
		attachment.EgressTCAttached = tcFiltersAttached(uplinks, netlink.HANDLE_MIN_EGRESS)
	}

	if localOutCompat {
		attachment.LocalhostLinksAttached = localhostLinksAttached(cfg.PinPath)
	}

	return attachment
}

func pinnedMapsReady(pinPath string) bool {
	required := []string{
		serviceMapName,
		statsMapName,
		localAddrMapName,
		revNatMapName,
		configMapName,
		hostNetnsCookieMapName,
		uplinkAddrMapName,
		nativeRouteMapName,
		snatFwdMapName,
		snatRevMapName,
		localhostSockMapName,
	}
	for _, name := range required {
		loaded, err := ebpf.LoadPinnedMap(filepath.Join(pinPath, name), nil)
		if err != nil {
			return false
		}
		_ = loaded.Close()
	}
	return true
}

func pinnedProgramsReady(pinPath string) bool {
	required := []string{
		filepath.Join(pinPath, "programs", "ingress"),
		filepath.Join(pinPath, "programs", "egress"),
		filepath.Join(pinPath, "programs", "localhost-connect4"),
		filepath.Join(pinPath, "programs", "localhost-getpeer4"),
		filepath.Join(pinPath, "programs", "localhost-release"),
	}
	for _, path := range required {
		loaded, err := ebpf.LoadPinnedProgram(path, nil)
		if err != nil {
			return false
		}
		_ = loaded.Close()
	}
	return true
}

func lookupKernelStat(statsMap *ebpf.Map, index uint32) uint64 {
	var perCPU []uint64
	if err := statsMap.Lookup(index, &perCPU); err != nil {
		return 0
	}
	var total uint64
	for _, value := range perCPU {
		total += value
	}
	return total
}

func tcFiltersAttached(devices []string, parent uint32) bool {
	if len(devices) == 0 {
		return false
	}
	for _, device := range devices {
		linkHandle, err := netlink.LinkByName(device)
		if err != nil {
			return false
		}
		filters, err := netlink.FilterList(linkHandle, parent)
		if err != nil {
			return false
		}
		found := false
		for _, filter := range filters {
			if _, ok := filter.(*netlink.BpfFilter); ok {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func localhostLinksAttached(pinPath string) bool {
	required := []string{
		filepath.Join(pinPath, "links", "localhost-connect4"),
		filepath.Join(pinPath, "links", "localhost-getpeer4"),
		filepath.Join(pinPath, "links", "localhost-release"),
	}
	for _, path := range required {
		pinned, err := link.LoadPinnedLink(path, nil)
		if err != nil {
			return false
		}
		_ = pinned.Close()
	}
	return true
}
