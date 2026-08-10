package placement

import (
	"context"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
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
	req, err := p.buildRequest(env, config)
	if err != nil {
		return nil, err
	}
	now := p.now()
	candidates, rejected, snapshot, err := p.planEligibleCandidates(req, now)
	if err != nil {
		p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultError, 0, rejected)
		return nil, err
	}
	if len(candidates) == 0 {
		retryable := retryableCandidatesFromRejected(snapshot, rejected, req, now)
		if len(retryable) == 0 {
			p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultNoEligible, 0, rejected)
			return nil, placementkernel.NoEligibleNodeError(req, rejected)
		}
		p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultRetryable, len(retryable), rejected)
		return retryable, nil
	}
	p.observeSelection(ctx, req, SelectionModeCandidates, SelectionResultOK, len(candidates), rejected)
	return candidates, nil
}

func (p *Selector) planEligibleCandidates(req *placementkernel.Request, now time.Time) ([]*placementkernel.Candidate, []*nodev1.PlacementCandidate, nodekernel.Snapshot, error) {
	snapshot := p.registry.Snapshot()
	eligible, rejected := p.engine.Plan(snapshot, req, now)
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
			candidateRequest, resolveErr := requestForCandidate(req, record, now)
			if resolveErr != nil {
				return nil, rejected, snapshot, resolveErr
			}
			out = append(out, &placementkernel.Candidate{Record: record, Evaluation: evaluation, BaseRequest: req, Request: candidateRequest})
		}
	}
	if len(out) == 0 {
		return nil, rejected, snapshot, grpcstatus.Error(codes.Internal, "eligible nodes disappeared")
	}
	return out, rejected, snapshot, nil
}

func (p *Selector) observeSelection(ctx context.Context, req *placementkernel.Request, mode, result string, eligibleCount int, rejected []*nodev1.PlacementCandidate) {
	if p.observer == nil {
		return
	}
	p.observer.RecordSelection(ctx, SelectionObservation{
		Mode:                           mode,
		Result:                         result,
		Runtime:                        req.GetRuntime(),
		MountType:                      req.GetMountType(),
		RequestedCPUMilli:              req.GetRequestedCpuMilli(),
		RequestedMemoryBytes:           req.GetRequestedMemoryBytes(),
		RequestedEphemeralStorageBytes: req.GetRequestedEphemeralStorageBytes(),
		EligibleCount:                  eligibleCount,
		RejectedCount:                  len(rejected),
		RejectionReasons:               placementkernel.CandidateRejectionReasons(rejected),
	})
}

func retryableCandidatesFromRejected(snapshot nodekernel.Snapshot, rejected []*nodev1.PlacementCandidate, req *placementkernel.Request, now time.Time) []*placementkernel.Candidate {
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
			candidateRequest, err := requestForCandidate(req, record, now)
			if err != nil {
				continue
			}
			out = append(out, &placementkernel.Candidate{Record: record, Evaluation: evaluation, BaseRequest: req, Request: candidateRequest})
		}
	}
	return out
}

func requestForCandidate(request *placementkernel.Request, record *nodekernel.Record, now time.Time) (*placementkernel.Request, error) {
	if request == nil || record == nil {
		return nil, nil
	}
	return placementkernel.ResolveRequestForNode(request, record.Summary, now)
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
