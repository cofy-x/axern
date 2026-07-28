package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/network/bpfnet/internal/inspect"
)

func TestWriteStatusHumanIncludesFallbackAndServices(t *testing.T) {
	status := bpfnet.Status{
		State: bpfnet.DataplaneState{
			Mode:             bpfnet.ModeIPTablesFullFallback,
			PinPath:          "/tmp/pins",
			IPRange:          "172.17.0.1/16",
			SNATPortMin:      bpfnet.SNATAllocatorPortMin,
			SNATPortMax:      bpfnet.SNATAllocatorPortMax,
			SNATPortAttempts: bpfnet.SNATAllocatorPortAttempts,
			FullFallback:     true,
			LastAttachError:  "attach failed",
			LastTCProbeError: "tc probe failed",
		},
		Services: []bpfnet.Service{{
			Protocol:   "tcp",
			HostPort:   18080,
			TargetIP:   "172.17.0.2",
			TargetPort: 80,
		}},
		Stats: bpfnet.Stats{Fallbacks: 1, AttachErrors: 1},
		Kernel: bpfnet.KernelStats{
			ServiceHits:                        3,
			SNATAllocExhausted:                 11,
			SNATTCPNonSynMisses:                13,
			SNATTCPNonSynMissFINs:              2,
			SNATTCPNonSynMissRSTs:              3,
			SNATTCPNonSynMissACKs:              5,
			SNATTCPNonSynMissOther:             7,
			SNATFullCloseReclaims:              17,
			SNATFullCloseMarks:                 19,
			SNATTCPFullCloseDeletes:            23,
			SNATTCPFullCloseDeletesFwd:         11,
			SNATTCPFullCloseDeletesRev:         12,
			SNATTCPNonSynMissFwdLookups:        29,
			SNATTCPNonSynMissFwdHostMismatches: 31,
			SNATTCPReverseMisses:               37,
			SNATTCPReverseMissSynACKs:          41,
			SNATTCPReverseMissFINs:             43,
			SNATTCPReverseMissRSTs:             47,
			SNATTCPReverseMissACKs:             53,
			SNATTCPReverseMissOther:            59,
			AttachErrors:                       1,
		},
		SNATMaps: bpfnet.SNATMapStats{
			FwdEntries:             4,
			FwdTCPEntries:          1,
			FwdUDPEntries:          2,
			FwdICMPEntries:         1,
			FwdActiveEntries:       1,
			FwdClosingEntries:      3,
			FwdOrigClosingEntries:  1,
			FwdReplyClosingEntries: 1,
			FwdFullClosingEntries:  1,
			RevEntries:             6,
			RevTCPEntries:          2,
			RevUDPEntries:          3,
			RevICMPEntries:         1,
			RevActiveEntries:       2,
			RevClosingEntries:      4,
			RevOrigClosingEntries:  1,
			RevReplyClosingEntries: 1,
			RevFullClosingEntries:  2,
			TranslatedPortsUsed:    3,
			UDPTranslatedPortsUsed: 2,
		},
	}

	var buf bytes.Buffer
	if err := writeStatus(&buf, status); err != nil {
		t.Fatalf("write status: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"mode: iptables-full-fallback",
		"localhost_compat_fallback: no",
		"snat_port_range: 10000-65535",
		"snat_port_attempts: 256",
		"full_fallback: yes",
		"last_attach_error: attach failed",
		"tcp host:18080 -> 172.17.0.2:80",
		"fwd_entries: 4",
		"fwd_tcp_entries: 1",
		"fwd_udp_entries: 2",
		"fwd_icmp_entries: 1",
		"fwd_active_entries: 1",
		"fwd_closing_entries: 3",
		"fwd_orig_closing_entries: 1",
		"fwd_reply_closing_entries: 1",
		"fwd_full_closing_entries: 1",
		"rev_entries: 6",
		"rev_tcp_entries: 2",
		"rev_udp_entries: 3",
		"rev_icmp_entries: 1",
		"rev_active_entries: 2",
		"rev_closing_entries: 4",
		"rev_orig_closing_entries: 1",
		"rev_reply_closing_entries: 1",
		"rev_full_closing_entries: 2",
		"translated_ports_used: 3",
		"udp_translated_ports_used: 2",
		"service_hits: 3",
		"snat_alloc_exhausted: 11",
		"snat_tcp_non_syn_misses: 13",
		"snat_tcp_non_syn_miss_fins: 2",
		"snat_tcp_non_syn_miss_rsts: 3",
		"snat_tcp_non_syn_miss_acks: 5",
		"snat_tcp_non_syn_miss_other: 7",
		"snat_full_close_reclaims: 17",
		"snat_full_close_marks: 19",
		"snat_tcp_full_close_deletes: 23",
		"snat_tcp_full_close_deletes_fwd: 11",
		"snat_tcp_full_close_deletes_rev: 12",
		"snat_tcp_non_syn_miss_fwd_lookups: 29",
		"snat_tcp_non_syn_miss_fwd_host_mismatches: 31",
		"snat_tcp_reverse_misses: 37",
		"snat_tcp_reverse_miss_syn_acks: 41",
		"snat_tcp_reverse_miss_fins: 43",
		"snat_tcp_reverse_miss_rsts: 47",
		"snat_tcp_reverse_miss_acks: 53",
		"snat_tcp_reverse_miss_other: 59",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestWriteObjectsHumanIncludesMissingMap(t *testing.T) {
	objects := []inspect.ObjectInfo{{
		Kind:    "map",
		Name:    "service_map",
		Path:    "/tmp/pins/service_map",
		Present: false,
		Error:   "missing",
	}}

	var buf bytes.Buffer
	if err := writeObjects(&buf, objects); err != nil {
		t.Fatalf("write objects: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "service_map") || !strings.Contains(out, "missing") {
		t.Fatalf("unexpected objects output:\n%s", out)
	}
}

func TestWriteDumpHuman(t *testing.T) {
	dump := inspect.Dump{
		MapName: "service_map",
		Limit:   1,
		Entries: []inspect.Entry{{
			Key:   map[string]any{"protocol": "tcp", "host_port": 18080},
			Value: map[string]any{"target_ip": "172.17.0.2", "target_port": 80},
		}},
	}

	var buf bytes.Buffer
	if err := writeDump(&buf, dump); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "map: service_map") || !strings.Contains(out, "172.17.0.2") {
		t.Fatalf("unexpected dump output:\n%s", out)
	}
}
