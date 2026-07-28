package bpfnetstatus

import (
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
)

func TestKernelDelta(t *testing.T) {
	before := bpfnet.KernelStats{
		AttachSuccesses:                    2,
		ServiceHits:                        10,
		SNATHits:                           7,
		SNATFwdHits:                        4,
		SNATMappingsProgrammed:             3,
		SNATFallbackHits:                   3,
		SNATAllocCollisions:                2,
		SNATAllocExhausted:                 2,
		SNATTCPNonSynMisses:                7,
		SNATTCPNonSynMissFINs:              3,
		SNATTCPNonSynMissRSTs:              2,
		SNATTCPNonSynMissACKs:              1,
		SNATTCPNonSynMissOther:             1,
		SNATFullCloseReclaims:              2,
		SNATFullCloseMarks:                 11,
		SNATTCPFullCloseDeletes:            4,
		SNATTCPFullCloseDeletesFwd:         2,
		SNATTCPFullCloseDeletesRev:         2,
		SNATTCPNonSynMissFwdLookups:        6,
		SNATTCPNonSynMissFwdHostMismatches: 1,
		SNATTCPReverseMisses:               9,
		SNATTCPReverseMissSynACKs:          3,
		SNATTCPReverseMissFINs:             2,
		SNATTCPReverseMissRSTs:             2,
		SNATTCPReverseMissACKs:             1,
		SNATTCPReverseMissOther:            1,
		LocalhostConnectHits:               5,
	}
	after := bpfnet.KernelStats{
		AttachSuccesses:                    3,
		ServiceHits:                        16,
		SNATHits:                           9,
		SNATFwdHits:                        10,
		SNATMappingsProgrammed:             9,
		SNATFallbackHits:                   4,
		SNATAllocCollisions:                5,
		SNATAllocExhausted:                 2,
		SNATTCPNonSynMisses:                11,
		SNATTCPNonSynMissFINs:              4,
		SNATTCPNonSynMissRSTs:              5,
		SNATTCPNonSynMissACKs:              2,
		SNATTCPNonSynMissOther:             0,
		SNATFullCloseReclaims:              9,
		SNATFullCloseMarks:                 13,
		SNATTCPFullCloseDeletes:            10,
		SNATTCPFullCloseDeletesFwd:         7,
		SNATTCPFullCloseDeletesRev:         3,
		SNATTCPNonSynMissFwdLookups:        12,
		SNATTCPNonSynMissFwdHostMismatches: 1,
		SNATTCPReverseMisses:               14,
		SNATTCPReverseMissSynACKs:          4,
		SNATTCPReverseMissFINs:             5,
		SNATTCPReverseMissRSTs:             3,
		SNATTCPReverseMissACKs:             1,
		SNATTCPReverseMissOther:            0,
		LocalhostConnectHits:               4,
	}

	delta := KernelDelta(before, after)
	if delta.AttachSuccesses != 1 || delta.ServiceHits != 6 || delta.SNATHits != 2 {
		t.Fatalf("unexpected positive kernel delta: %#v", delta)
	}
	if delta.SNATFwdHits != 6 || delta.SNATMappingsProgrammed != 6 || delta.SNATFallbackHits != 1 || delta.SNATAllocCollisions != 3 {
		t.Fatalf("unexpected snat kernel delta: %#v", delta)
	}
	if delta.SNATAllocExhausted != 0 || delta.SNATTCPNonSynMisses != 4 || delta.SNATFullCloseReclaims != 7 || delta.SNATFullCloseMarks != 2 {
		t.Fatalf("unexpected snat allocator delta: %#v", delta)
	}
	if delta.SNATTCPNonSynMissFINs != 1 || delta.SNATTCPNonSynMissRSTs != 3 || delta.SNATTCPNonSynMissACKs != 1 || delta.SNATTCPNonSynMissOther != 0 {
		t.Fatalf("unexpected tcp non-syn miss class delta: %#v", delta)
	}
	if delta.SNATTCPFullCloseDeletes != 6 || delta.SNATTCPFullCloseDeletesFwd != 5 || delta.SNATTCPFullCloseDeletesRev != 1 {
		t.Fatalf("unexpected tcp full-close delete delta: %#v", delta)
	}
	if delta.SNATTCPNonSynMissFwdLookups != 6 || delta.SNATTCPNonSynMissFwdHostMismatches != 0 {
		t.Fatalf("unexpected tcp non-syn miss reason delta: %#v", delta)
	}
	if delta.SNATTCPReverseMisses != 5 || delta.SNATTCPReverseMissSynACKs != 1 || delta.SNATTCPReverseMissFINs != 3 || delta.SNATTCPReverseMissRSTs != 1 || delta.SNATTCPReverseMissACKs != 0 || delta.SNATTCPReverseMissOther != 0 {
		t.Fatalf("unexpected tcp reverse miss delta: %#v", delta)
	}
	if delta.LocalhostConnectHits != 0 {
		t.Fatalf("expected saturating localhost delta, got %#v", delta)
	}
}

func TestRequireTCReady(t *testing.T) {
	status := bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady:      true,
			FullFallback: false,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}
	if err := RequireTCReady(status); err != nil {
		t.Fatalf("expected tc ready status to validate, got %v", err)
	}
}

func TestRequireLocalhostTCPReadyRejectsCompatFallback(t *testing.T) {
	status := bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady:            true,
			LocalhostTCPDNAT:   false,
			LocalhostPathReady: false,
			LocalhostCompat:    true,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}
	if err := RequireLocalhostTCPReady(status); err == nil {
		t.Fatalf("expected localhost compat fallback to be rejected")
	}
}
