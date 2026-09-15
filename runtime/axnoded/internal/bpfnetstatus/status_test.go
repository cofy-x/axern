package bpfnetstatus

import (
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
)

func TestKernelDeltaSaturatesAndTracksSNAT(t *testing.T) {
	before := bpfnet.KernelStats{SNATHits: 10, SNATRevHits: 8, AttachErrors: 2}
	after := bpfnet.KernelStats{SNATHits: 16, SNATRevHits: 7, AttachErrors: 3}
	delta := KernelDelta(before, after)
	if delta.SNATHits != 6 || delta.SNATRevHits != 0 || delta.AttachErrors != 1 {
		t.Fatalf("unexpected delta: %#v", delta)
	}
}

func TestRequireTCReadyRequiresCompleteAttachment(t *testing.T) {
	status := bpfnet.Status{
		State: bpfnet.DataplaneState{TCReady: true},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}
	if err := RequireTCReady(status); err != nil {
		t.Fatal(err)
	}
	status.Attachment.EgressTCAttached = false
	if err := RequireTCReady(status); err == nil {
		t.Fatal("missing TC attachment was accepted")
	}
}
