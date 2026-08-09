package placementkernel

import (
	"strings"
	"testing"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNoEligibleNodeErrorClassifiesCapabilityInvalidationAsNodeSelection(t *testing.T) {
	err := NoEligibleNodeError(&Request{RequestedMemoryBytes: 512 << 20}, []*nodev1.PlacementCandidate{{
		NodeID:           "node-a",
		State:            nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_REJECTED,
		RejectionReasons: []nodev1.PlacementRejectionReason{nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED},
	}})
	st := grpcstatus.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", st.Code())
	}
	if !strings.Contains(st.Message(), "rejection_reasons=capability_unsupported") {
		t.Fatalf("message = %q, want capability rejection", st.Message())
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if info.GetReason() != string(resourcekernel.AdmissionRejectionNodeSelection) {
			t.Fatalf("reason = %q, want %q", info.GetReason(), resourcekernel.AdmissionRejectionNodeSelection)
		}
		if info.GetMetadata()["diagnostic_code"] != string(resourcekernel.AdmissionDiagnosticNodeSelection) {
			t.Fatalf("diagnostic_code = %q", info.GetMetadata()["diagnostic_code"])
		}
		return
	}
	t.Fatal("missing ErrorInfo detail")
}
