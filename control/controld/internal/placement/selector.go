package placement

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Selector struct {
	registry              *nodekernel.Registry
	engine                *Engine
	observer              Observer
	now                   func() time.Time
	defaultSandboxRuntime string
}

func NewSelector(registry *nodekernel.Registry, engine *Engine, now func() time.Time, defaultSandboxRuntime string) *Selector {
	return &Selector{
		registry:              registry,
		engine:                engine,
		now:                   now,
		defaultSandboxRuntime: defaultSandboxRuntime,
	}
}

func (p *Selector) WithObserver(observer Observer) *Selector {
	p.observer = observer
	return p
}

func (p *Selector) SelectCandidates(ctx context.Context, env *environmentv1.Environment, config *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error) {
	req := p.buildRequest(env, config)
	candidates, rejected, snapshot, err := p.planEligibleCandidates(req)
	if err != nil {
		p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultError, 0, rejected)
		return nil, err
	}
	if len(candidates) == 0 {
		retryable := retryableCandidatesFromRejected(snapshot, rejected)
		if len(retryable) == 0 {
			p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultNoEligible, 0, rejected)
			return nil, noEligibleNodeError(req, rejected)
		}
		p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultRetryable, len(retryable), rejected)
		return retryable, nil
	}
	p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultOK, len(candidates), rejected)
	return candidates, nil
}

func noEligibleNodeError(req *Request, rejected []*nodev1.PlacementCandidate) error {
	reasons := candidateRejectionReasons(rejected)
	reason := placementAdmissionReason(rejected)
	st := grpcstatus.New(codes.FailedPrecondition, noEligibleNodeMessage(req, rejected))
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: string(reason),
		Domain: resourcekernel.AdmissionErrorDomain,
		Metadata: map[string]string{
			"diagnostic_code":        string(resourcekernel.AdmissionDiagnosticForReason(reason)),
			"requested_cpu_milli":    fmt.Sprintf("%d", req.GetRequestedCpuMilli()),
			"requested_memory_bytes": fmt.Sprintf("%d", req.GetRequestedMemoryBytes()),
			"rejection_reasons":      strings.Join(rejectionReasonLabels(reasons), ","),
		},
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func placementAdmissionReason(rejected []*nodev1.PlacementCandidate) resourcekernel.AdmissionRejectionReason {
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
			nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY:
			hasCapacityReason = true
		case nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_UNSPECIFIED:
		default:
			return false
		}
	}
	return hasCapacityReason
}

func noEligibleNodeMessage(req *Request, rejected []*nodev1.PlacementCandidate) string {
	message := fmt.Sprintf("no eligible node: requested cpu_milli=%d memory_bytes=%d", req.GetRequestedCpuMilli(), req.GetRequestedMemoryBytes())
	reasons := rejectionReasonLabels(candidateRejectionReasons(rejected))
	if len(reasons) > 0 {
		message += "; rejection_reasons=" + strings.Join(reasons, ",")
	}
	return message
}

func rejectionReasonLabels(reasons []nodev1.PlacementRejectionReason) []string {
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

func (p *Selector) planEligibleCandidates(req *Request) ([]*placementkernel.Candidate, []*nodev1.PlacementCandidate, nodekernel.Snapshot, error) {
	snapshot := p.registry.Snapshot()
	eligible, rejected := p.engine.Plan(snapshot, req, p.now())
	if len(eligible) == 0 {
		return nil, rejected, snapshot, nil
	}
	byID := make(map[string]*nodekernel.Record, len(snapshot.Records))
	for _, record := range snapshot.Records {
		if record != nil {
			byID[record.NodeID] = record
		}
	}
	out := make([]*placementkernel.Candidate, 0, len(eligible))
	for _, evaluation := range eligible {
		if record := byID[strings.TrimSpace(evaluation.GetNodeID())]; record != nil {
			out = append(out, &placementkernel.Candidate{Record: record, Evaluation: evaluation})
		}
	}
	if len(out) == 0 {
		return nil, rejected, snapshot, grpcstatus.Error(codes.Internal, "eligible nodes disappeared")
	}
	return out, rejected, snapshot, nil
}

func (p *Selector) observeSelection(ctx context.Context, req *Request, mode, result string, eligibleCount int, rejected []*nodev1.PlacementCandidate) {
	if p.observer == nil {
		return
	}
	p.observer.RecordSelection(ctx, SelectionObservation{
		Mode:                 mode,
		Result:               result,
		Runtime:              req.GetRuntime(),
		MountType:            req.GetMountType(),
		RequestedCPUMilli:    req.GetRequestedCpuMilli(),
		RequestedMemoryBytes: req.GetRequestedMemoryBytes(),
		EligibleCount:        eligibleCount,
		RejectedCount:        len(rejected),
		RejectionReasons:     candidateRejectionReasons(rejected),
	})
}

func candidateRejectionReasons(candidates []*nodev1.PlacementCandidate) []nodev1.PlacementRejectionReason {
	reasons := make([]nodev1.PlacementRejectionReason, 0)
	for _, candidate := range candidates {
		reasons = append(reasons, candidate.GetRejectionReasons()...)
	}
	return dedupeRejectionReasons(reasons)
}

func retryableCandidatesFromRejected(snapshot nodekernel.Snapshot, rejected []*nodev1.PlacementCandidate) []*placementkernel.Candidate {
	if len(rejected) == 0 || len(snapshot.Records) == 0 {
		return nil
	}
	byID := make(map[string]*nodekernel.Record, len(snapshot.Records))
	for _, record := range snapshot.Records {
		if record == nil {
			continue
		}
		byID[strings.TrimSpace(record.NodeID)] = record
	}
	out := make([]*placementkernel.Candidate, 0, len(rejected))
	for _, evaluation := range rejected {
		if !retryableRejection(evaluation.GetRejectionReasons()) {
			continue
		}
		if record := byID[strings.TrimSpace(evaluation.GetNodeID())]; record != nil {
			out = append(out, &placementkernel.Candidate{Record: record, Evaluation: evaluation})
		}
	}
	return out
}

func retryableRejection(reasons []nodev1.PlacementRejectionReason) bool {
	if len(reasons) == 0 {
		return false
	}
	for _, reason := range reasons {
		if !retryableRejectionReason(reason) {
			return false
		}
	}
	return true
}

func retryableRejectionReason(reason nodev1.PlacementRejectionReason) bool {
	switch reason {
	case nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_HEARTBEAT,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_SUMMARY,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_AXNODED_NOT_READY,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE:
		return true
	case nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_UNSPECIFIED:
		return true
	default:
		return false
	}
}
