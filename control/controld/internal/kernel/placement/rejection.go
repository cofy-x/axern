package placementkernel

import (
	"fmt"
	"sort"
	"strings"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// NoEligibleNodeError renders the canonical placement rejection used both by
// optimistic candidate selection and by durable admission after node rows are
// locked. Keeping this at the policy boundary prevents the transaction path
// from misclassifying a refreshed eligibility failure as reservation capacity.
func NoEligibleNodeError(req *Request, rejected []*nodev1.PlacementCandidate) error {
	reasons := CandidateRejectionReasons(rejected)
	reason := admissionReason(rejected)
	st := grpcstatus.New(codes.FailedPrecondition, noEligibleNodeMessage(req, reasons))
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: string(reason),
		Domain: resourcekernel.AdmissionErrorDomain,
		Metadata: map[string]string{
			"diagnostic_code":                   string(resourcekernel.AdmissionDiagnosticForReason(reason)),
			"requested_cpu_milli":               fmt.Sprintf("%d", req.GetRequestedCpuMilli()),
			"requested_memory_bytes":            fmt.Sprintf("%d", req.GetRequestedMemoryBytes()),
			"requested_ephemeral_storage_bytes": fmt.Sprintf("%d", req.GetRequestedEphemeralStorageBytes()),
			"rejection_reasons":                 strings.Join(RejectionReasonLabels(reasons), ","),
		},
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func CandidateRejectionReasons(candidates []*nodev1.PlacementCandidate) []nodev1.PlacementRejectionReason {
	seen := make(map[nodev1.PlacementRejectionReason]struct{})
	reasons := make([]nodev1.PlacementRejectionReason, 0)
	for _, candidate := range candidates {
		for _, reason := range candidate.GetRejectionReasons() {
			if _, ok := seen[reason]; ok {
				continue
			}
			seen[reason] = struct{}{}
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func RejectionReasonLabels(reasons []nodev1.PlacementRejectionReason) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		label := strings.ToLower(strings.TrimPrefix(reason.String(), "PLACEMENT_REJECTION_REASON_"))
		if label == "" || label == "unspecified" {
			continue
		}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func admissionReason(rejected []*nodev1.PlacementCandidate) resourcekernel.AdmissionRejectionReason {
	if len(rejected) == 0 {
		return resourcekernel.AdmissionRejectionNodeSelection
	}
	for _, candidate := range rejected {
		if !capacityOnlyRejection(candidate.GetRejectionReasons()) {
			return resourcekernel.AdmissionRejectionNodeSelection
		}
	}
	return resourcekernel.AdmissionRejectionPlacementCapacity
}

func capacityOnlyRejection(reasons []nodev1.PlacementRejectionReason) bool {
	hasCapacityReason := false
	for _, reason := range reasons {
		switch reason {
		case nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU,
			nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY,
			nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_EPHEMERAL_STORAGE:
			hasCapacityReason = true
		case nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_UNSPECIFIED:
		default:
			return false
		}
	}
	return hasCapacityReason
}

func noEligibleNodeMessage(req *Request, reasons []nodev1.PlacementRejectionReason) string {
	message := fmt.Sprintf("no eligible node: requested cpu_milli=%d memory_bytes=%d ephemeral_storage_bytes=%d", req.GetRequestedCpuMilli(), req.GetRequestedMemoryBytes(), req.GetRequestedEphemeralStorageBytes())
	labels := RejectionReasonLabels(reasons)
	if len(labels) > 0 {
		message += "; rejection_reasons=" + strings.Join(labels, ",")
	}
	return message
}
