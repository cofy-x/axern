package placementkernel

import (
	"strings"
	"testing"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNoEligibleNodeErrorClassifiesCapabilityInvalidationAsNodeSelection(t *testing.T) {
	err := NoEligibleNodeError(&Request{RequestedMemoryBytes: 512 << 20}, []*Evaluation{{
		NodeID:           "node-a",
		State:            CandidateStateRejected,
		RejectionReasons: []RejectionReason{RejectionReasonCapabilityUnsupported},
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
