package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/network/bpfnet/internal/inspect"
)

func writeStatus(w io.Writer, status bpfnet.Status) error {
	state := status.State
	fmt.Fprintln(w, "bpfnet status")
	fmt.Fprintf(w, "  mode: %s\n", emptyDash(state.Mode))
	fmt.Fprintf(w, "  pin_path: %s\n", emptyDash(state.PinPath))
	fmt.Fprintf(w, "  ip_range: %s\n", emptyDash(state.IPRange))
	fmt.Fprintf(w, "  map_size: %d\n", state.MapSize)
	fmt.Fprintf(w, "  snat_map_size: %d\n", state.SNATMapSize)
	fmt.Fprintf(w, "  snat_port_range: %d-%d\n", state.SNATPortMin, state.SNATPortMax)
	fmt.Fprintf(w, "  snat_port_attempts: %d\n", state.SNATPortAttempts)
	fmt.Fprintf(w, "  native_routes: %s\n", joinOrDash(state.NativeRoutingCIDRs))
	fmt.Fprintf(w, "  iptables_safety_fallback: %s\n", boolWord(state.IptablesFallback))
	fmt.Fprintf(w, "  tc_ready: %s\n", boolWord(state.TCReady))
	fmt.Fprintf(w, "  local_out_compat: %s\n", boolWord(state.LocalOutCompat))
	fmt.Fprintf(w, "  localhost_tcp_dnat: %s\n", boolWord(state.LocalhostTCPDNAT))
	fmt.Fprintf(w, "  localhost_path_ready: %s\n", boolWord(state.LocalhostPathReady))
	fmt.Fprintf(w, "  localhost_compat_fallback: %s\n", boolWord(state.LocalhostCompat))
	fmt.Fprintf(w, "  full_fallback: %s\n", boolWord(state.FullFallback))
	if state.LastAttachError != "" {
		fmt.Fprintf(w, "  last_attach_error: %s\n", state.LastAttachError)
	}
	if state.LastTCProbeError != "" {
		fmt.Fprintf(w, "  last_tc_probe_error: %s\n", state.LastTCProbeError)
	}
	if state.LastReconcileError != "" {
		fmt.Fprintf(w, "  last_reconcile_error: %s\n", state.LastReconcileError)
	}
	if state.LastLocalhostError != "" {
		fmt.Fprintf(w, "  last_localhost_error: %s\n", state.LastLocalhostError)
	}

	attachment := status.Attachment
	fmt.Fprintln(w)
	fmt.Fprintln(w, "attachment")
	fmt.Fprintf(w, "  uplinks: %s\n", joinOrDash(attachment.UplinkDevices))
	fmt.Fprintf(w, "  local_addresses: %s\n", joinOrDash(attachment.LocalAddresses))
	fmt.Fprintf(w, "  ingress_tc: %s\n", boolWord(attachment.IngressTCAttached))
	fmt.Fprintf(w, "  egress_tc: %s\n", boolWord(attachment.EgressTCAttached))
	fmt.Fprintf(w, "  localhost_links: %s\n", boolWord(attachment.LocalhostLinksAttached))
	fmt.Fprintf(w, "  pinned_maps: %s\n", boolWord(attachment.PinnedMapsReady))
	fmt.Fprintf(w, "  pinned_programs: %s\n", boolWord(attachment.PinnedProgramsReady))
	if !attachment.PinnedProgramsReady && (attachment.IngressTCAttached || attachment.EgressTCAttached || attachment.LocalhostLinksAttached) {
		fmt.Fprintln(w, "  note: dataplane links are attached, but pinned program objects are missing")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "services")
	if len(status.Services) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		for _, svc := range status.Services {
			fmt.Fprintf(w, "  %s host:%d -> %s:%d\n", svc.Protocol, svc.HostPort, svc.TargetIP, svc.TargetPort)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "stats")
	fmt.Fprintf(w, "  attach_successes: %d\n", status.Stats.AttachSuccesses)
	fmt.Fprintf(w, "  upserts: %d\n", status.Stats.Upserts)
	fmt.Fprintf(w, "  deletes: %d\n", status.Stats.Deletes)
	fmt.Fprintf(w, "  conflicts: %d\n", status.Stats.Conflicts)
	fmt.Fprintf(w, "  fallbacks: %d\n", status.Stats.Fallbacks)
	fmt.Fprintf(w, "  attach_errors: %d\n", status.Stats.AttachErrors)

	snatMaps := status.SNATMaps
	fmt.Fprintln(w)
	fmt.Fprintln(w, "snat maps")
	fmt.Fprintf(w, "  fwd_entries: %d\n", snatMaps.FwdEntries)
	fmt.Fprintf(w, "  fwd_tcp_entries: %d\n", snatMaps.FwdTCPEntries)
	fmt.Fprintf(w, "  fwd_udp_entries: %d\n", snatMaps.FwdUDPEntries)
	fmt.Fprintf(w, "  fwd_icmp_entries: %d\n", snatMaps.FwdICMPEntries)
	fmt.Fprintf(w, "  fwd_active_entries: %d\n", snatMaps.FwdActiveEntries)
	fmt.Fprintf(w, "  fwd_closing_entries: %d\n", snatMaps.FwdClosingEntries)
	fmt.Fprintf(w, "  fwd_orig_closing_entries: %d\n", snatMaps.FwdOrigClosingEntries)
	fmt.Fprintf(w, "  fwd_reply_closing_entries: %d\n", snatMaps.FwdReplyClosingEntries)
	fmt.Fprintf(w, "  fwd_full_closing_entries: %d\n", snatMaps.FwdFullClosingEntries)
	fmt.Fprintf(w, "  rev_entries: %d\n", snatMaps.RevEntries)
	fmt.Fprintf(w, "  rev_tcp_entries: %d\n", snatMaps.RevTCPEntries)
	fmt.Fprintf(w, "  rev_udp_entries: %d\n", snatMaps.RevUDPEntries)
	fmt.Fprintf(w, "  rev_icmp_entries: %d\n", snatMaps.RevICMPEntries)
	fmt.Fprintf(w, "  rev_active_entries: %d\n", snatMaps.RevActiveEntries)
	fmt.Fprintf(w, "  rev_closing_entries: %d\n", snatMaps.RevClosingEntries)
	fmt.Fprintf(w, "  rev_orig_closing_entries: %d\n", snatMaps.RevOrigClosingEntries)
	fmt.Fprintf(w, "  rev_reply_closing_entries: %d\n", snatMaps.RevReplyClosingEntries)
	fmt.Fprintf(w, "  rev_full_closing_entries: %d\n", snatMaps.RevFullClosingEntries)
	fmt.Fprintf(w, "  rev_reverse_entries: %d\n", snatMaps.RevReverseEntries)
	fmt.Fprintf(w, "  rev_alias_entries: %d\n", snatMaps.RevAliasEntries)
	fmt.Fprintf(w, "  translated_ports_used: %d\n", snatMaps.TranslatedPortsUsed)
	fmt.Fprintf(w, "  udp_translated_ports_used: %d\n", snatMaps.UDPTranslatedPortsUsed)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "kernel")
	fmt.Fprintf(w, "  service_hits: %d\n", status.Kernel.ServiceHits)
	fmt.Fprintf(w, "  rev_nat_hits: %d\n", status.Kernel.RevNATHits)
	fmt.Fprintf(w, "  snat_hits: %d\n", status.Kernel.SNATHits)
	fmt.Fprintf(w, "  snat_fwd_hits: %d\n", status.Kernel.SNATFwdHits)
	fmt.Fprintf(w, "  snat_rev_hits: %d\n", status.Kernel.SNATRevHits)
	fmt.Fprintf(w, "  snat_mappings_programmed: %d\n", status.Kernel.SNATMappingsProgrammed)
	fmt.Fprintf(w, "  snat_alloc_exhausted: %d\n", status.Kernel.SNATAllocExhausted)
	fmt.Fprintf(w, "  snat_tcp_non_syn_misses: %d\n", status.Kernel.SNATTCPNonSynMisses)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_fins: %d\n", status.Kernel.SNATTCPNonSynMissFINs)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_rsts: %d\n", status.Kernel.SNATTCPNonSynMissRSTs)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_acks: %d\n", status.Kernel.SNATTCPNonSynMissACKs)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_other: %d\n", status.Kernel.SNATTCPNonSynMissOther)
	fmt.Fprintf(w, "  snat_full_close_reclaims: %d\n", status.Kernel.SNATFullCloseReclaims)
	fmt.Fprintf(w, "  snat_full_close_marks: %d\n", status.Kernel.SNATFullCloseMarks)
	fmt.Fprintf(w, "  snat_tcp_full_close_deletes: %d\n", status.Kernel.SNATTCPFullCloseDeletes)
	fmt.Fprintf(w, "  snat_tcp_full_close_deletes_fwd: %d\n", status.Kernel.SNATTCPFullCloseDeletesFwd)
	fmt.Fprintf(w, "  snat_tcp_full_close_deletes_rev: %d\n", status.Kernel.SNATTCPFullCloseDeletesRev)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_fwd_lookups: %d\n", status.Kernel.SNATTCPNonSynMissFwdLookups)
	fmt.Fprintf(w, "  snat_tcp_non_syn_miss_fwd_host_mismatches: %d\n", status.Kernel.SNATTCPNonSynMissFwdHostMismatches)
	fmt.Fprintf(w, "  snat_tcp_reverse_misses: %d\n", status.Kernel.SNATTCPReverseMisses)
	fmt.Fprintf(w, "  snat_tcp_reverse_miss_syn_acks: %d\n", status.Kernel.SNATTCPReverseMissSynACKs)
	fmt.Fprintf(w, "  snat_tcp_reverse_miss_fins: %d\n", status.Kernel.SNATTCPReverseMissFINs)
	fmt.Fprintf(w, "  snat_tcp_reverse_miss_rsts: %d\n", status.Kernel.SNATTCPReverseMissRSTs)
	fmt.Fprintf(w, "  snat_tcp_reverse_miss_acks: %d\n", status.Kernel.SNATTCPReverseMissACKs)
	fmt.Fprintf(w, "  snat_tcp_reverse_miss_other: %d\n", status.Kernel.SNATTCPReverseMissOther)
	fmt.Fprintf(w, "  snat_fallback_hits: %d\n", status.Kernel.SNATFallbackHits)
	fmt.Fprintf(w, "  localhost_connect_hits: %d\n", status.Kernel.LocalhostConnectHits)
	fmt.Fprintf(w, "  localhost_getpeer_hits: %d\n", status.Kernel.LocalhostGetpeerHits)
	fmt.Fprintf(w, "  fallback_hits: %d\n", status.Kernel.FallbackHits)
	fmt.Fprintf(w, "  attach_errors: %d\n", status.Kernel.AttachErrors)
	return nil
}

func writeObjects(w io.Writer, objects []inspect.ObjectInfo) error {
	fmt.Fprintf(w, "%-8s %-28s %-7s %-8s %-14s %-8s %-8s %-8s %-7s %s\n", "KIND", "NAME", "PRESENT", "OPENABLE", "TYPE", "KEY", "VALUE", "MAX", "ENTRIES", "ERROR")
	for _, obj := range objects {
		fmt.Fprintf(
			w,
			"%-8s %-28s %-7s %-8s %-14s %-8d %-8d %-8d %-7d %s\n",
			obj.Kind,
			obj.Name,
			boolShort(obj.Present),
			boolShort(obj.Openable),
			emptyDash(obj.Type),
			obj.KeySize,
			obj.ValueSize,
			obj.MaxEntries,
			obj.Entries,
			obj.Error,
		)
	}
	return nil
}

func writeDump(w io.Writer, dump inspect.Dump) error {
	fmt.Fprintf(w, "map: %s\n", dump.MapName)
	fmt.Fprintf(w, "raw: %s\n", boolWord(dump.Raw))
	fmt.Fprintf(w, "entries: %d\n", len(dump.Entries))
	if dump.Truncated {
		fmt.Fprintf(w, "truncated: yes, limit=%d\n", dump.Limit)
	}
	for _, entry := range dump.Entries {
		key, _ := json.Marshal(entry.Key)
		value, _ := json.Marshal(entry.Value)
		fmt.Fprintf(w, "  %s => %s\n", key, value)
	}
	return nil
}

func boolWord(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func boolShort(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}
