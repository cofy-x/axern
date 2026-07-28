package inspect

import (
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
)

func TestListObjectsAgainstMissingPinPath(t *testing.T) {
	objects := ListObjects(t.TempDir())
	if len(objects) == 0 {
		t.Fatalf("expected known objects")
	}
	for _, object := range objects {
		if object.Present {
			t.Fatalf("expected object to be absent: %#v", object)
		}
	}
}

func TestHighChurnMapGuard(t *testing.T) {
	if !IsHighChurnMap(MapRevNAT) {
		t.Fatalf("expected rev_nat_map to be high churn")
	}
	if !IsHighChurnMap(MapSNATRevMarker) {
		t.Fatalf("expected snat_rev_marker_map to be high churn")
	}
	if IsHighChurnMap(MapService) {
		t.Fatalf("expected service_map not to be high churn")
	}
}

func TestStatsMapNamesMatchKernelIndexes(t *testing.T) {
	if got, want := len(statNames), int(bpfnet.KernelStatLocalhostFallbackHit)+1; got != want {
		t.Fatalf("expected %d stat names, got %d", want, got)
	}
	for index, want := range map[uint32]string{
		bpfnet.KernelStatSNATAllocExhausted:               "snat_alloc_exhausted",
		bpfnet.KernelStatSNATTCPNonSynMiss:                "snat_tcp_non_syn_miss",
		bpfnet.KernelStatSNATTCPNonSynMissFIN:             "snat_tcp_non_syn_miss_fin",
		bpfnet.KernelStatSNATTCPNonSynMissRST:             "snat_tcp_non_syn_miss_rst",
		bpfnet.KernelStatSNATTCPNonSynMissACK:             "snat_tcp_non_syn_miss_ack",
		bpfnet.KernelStatSNATTCPNonSynMissOther:           "snat_tcp_non_syn_miss_other",
		bpfnet.KernelStatSNATFullCloseReclaim:             "snat_full_close_reclaim",
		bpfnet.KernelStatSNATFullCloseMark:                "snat_full_close_mark",
		bpfnet.KernelStatSNATTCPFullCloseDelete:           "snat_tcp_full_close_delete",
		bpfnet.KernelStatSNATTCPFullCloseDeleteFwd:        "snat_tcp_full_close_delete_fwd",
		bpfnet.KernelStatSNATTCPFullCloseDeleteRev:        "snat_tcp_full_close_delete_rev",
		bpfnet.KernelStatSNATTCPNonSynMissFwdLookup:       "snat_tcp_non_syn_miss_fwd_lookup",
		bpfnet.KernelStatSNATTCPNonSynMissFwdHostMismatch: "snat_tcp_non_syn_miss_fwd_host_mismatch",
		bpfnet.KernelStatSNATTCPRevMiss:                   "snat_tcp_rev_miss",
		bpfnet.KernelStatSNATTCPRevMissSynACK:             "snat_tcp_rev_miss_syn_ack",
		bpfnet.KernelStatSNATTCPRevMissFIN:                "snat_tcp_rev_miss_fin",
		bpfnet.KernelStatSNATTCPRevMissRST:                "snat_tcp_rev_miss_rst",
		bpfnet.KernelStatSNATTCPRevMissACK:                "snat_tcp_rev_miss_ack",
		bpfnet.KernelStatSNATTCPRevMissOther:              "snat_tcp_rev_miss_other",
		bpfnet.KernelStatNativeRouteSkip:                  "native_route_skip",
	} {
		if got := statNames[index]; got != want {
			t.Fatalf("stat name at index %d: got %q, want %q", index, got, want)
		}
	}
}
