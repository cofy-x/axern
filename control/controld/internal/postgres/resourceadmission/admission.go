package resourceadmission

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/nodecapability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Admission struct {
	policy    resourcekernel.AdmissionPolicy
	placement placementkernel.Evaluator
}

const maxAdmissionRejectionDetails = 5

type AdmitCandidateRequest struct {
	Namespace     string
	EnvironmentID string
	Candidates    []*placementkernel.Candidate
	Config        *commonv1.ExecutionConfig
	Now           time.Time
}

func NewAdmission(policy resourcekernel.AdmissionPolicy, evaluator placementkernel.Evaluator) Admission {
	policy = resourcekernel.NormalizeAdmissionPolicy(policy)
	return Admission{policy: policy, placement: evaluator}
}

func (a Admission) Policy() resourcekernel.AdmissionPolicy { return a.policy }

func (a Admission) Evaluator() placementkernel.Evaluator { return a.placement }

func (a Admission) AdmitCandidate(ctx context.Context, tx pgx.Tx, req AdmitCandidateRequest) (_ *placementkernel.AdmissionDecision, retErr error) {
	totalStarted := time.Now()
	defer func() {
		recordResourceAdmissionStage(ctx, resourceAdmissionStageTotal, totalStarted, retErr)
	}()
	namespace := environmentkernel.NormalizeNamespace(req.Namespace)
	if a.placement == nil {
		return nil, fmt.Errorf("placement evaluator is required for durable admission")
	}
	requested := resourcekernel.QuantityToClaim(executionkernel.NormalizeConfig(req.Config).GetResources().GetRequests())
	nodeRequested := requested
	stageStarted := time.Now()
	quota, err := pgnamespace.LockQuotaPolicy(ctx, tx, namespace)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageLockNamespace, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	namespaceUsed, err := activeNamespaceAllocationUsage(ctx, tx, namespace)
	if err != nil {
		recordResourceAdmissionStage(ctx, resourceAdmissionStageEvaluateNamespace, stageStarted, err)
		return nil, err
	}
	quotaEvaluation := quota.EvaluateFit(namespaceUsed, requested)
	recordQuotaEvaluation(ctx, namespace, quotaEvaluation)
	if !quotaEvaluation.Fits() {
		rejection := quotaRejectionError(namespace, quotaEvaluation)
		if err := insertQuotaAdmissionRejectedEvent(ctx, tx, quotaAdmissionRejectedEvent{
			Namespace:     namespace,
			EnvironmentID: req.EnvironmentID,
			Evaluation:    quotaEvaluation,
			CreatedAt:     req.Now,
		}); err != nil {
			recordResourceAdmissionStage(ctx, resourceAdmissionStageEvaluateNamespace, stageStarted, err)
			return nil, err
		}
		recordResourceAdmissionStage(ctx, resourceAdmissionStageEvaluateNamespace, stageStarted, rejection)
		return nil, committedAdmissionError{err: rejection}
	}
	recordResourceAdmissionStage(ctx, resourceAdmissionStageEvaluateNamespace, stageStarted, nil)
	stageStarted = time.Now()
	locked, err := lockCandidateNodes(ctx, tx, req.Candidates)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageLockCandidates, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	usage, err := activeCandidateAllocationUsage(ctx, tx, locked)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageLoadAllocationCharges, stageStarted, err)
	if err != nil {
		return nil, err
	}
	// req.Now is the selection clock. Advance it by the real time spent waiting
	// for quota and node row locks so a short-lived observation cannot remain
	// eligible merely because admission was queued behind another transaction.
	lockedEvaluationTime := req.Now.Add(time.Since(totalStarted))
	stageStarted = time.Now()
	diagnostics := newAdmissionRejectionDiagnostics(maxAdmissionRejectionDetails)
	lockedEligibilityRejections := make([]*placementkernel.Evaluation, 0)
	var lockedRejectionRequest *placementkernel.Request
	candidatesEvaluated := 0
	var selected *placementkernel.Candidate
	for _, candidate := range req.Candidates {
		if candidate == nil || candidate.Record == nil {
			continue
		}
		record := locked[candidate.NodeID]
		if record == nil || !record.Active() {
			continue
		}
		baseRequest := candidate.BaseRequest
		if baseRequest == nil {
			baseRequest = candidate.Request
		}
		if baseRequest == nil {
			continue
		}
		freshRequest, resolveErr := placementkernel.ResolveRequestForNode(baseRequest, record.Summary, lockedEvaluationTime)
		if resolveErr != nil {
			freshEvaluation := a.placement.Evaluate(record, baseRequest, lockedEvaluationTime)
			if freshEvaluation != nil {
				lockedEligibilityRejections = append(lockedEligibilityRejections, freshEvaluation)
				if lockedRejectionRequest == nil {
					lockedRejectionRequest = baseRequest
				}
			}
			continue
		}
		freshEvaluation := a.placement.Evaluate(record, freshRequest, lockedEvaluationTime)
		if freshEvaluation == nil || freshEvaluation.GetState() != placementkernel.CandidateStateEligible {
			if freshEvaluation != nil {
				lockedEligibilityRejections = append(lockedEligibilityRejections, freshEvaluation)
				if lockedRejectionRequest == nil {
					lockedRejectionRequest = freshRequest
				}
			}
			if len(freshRequest.GetCapabilityRequirements()) > 0 &&
				!sameNodeObservationOrder(candidate.Record.Summary, record.Summary) {
				recordCapabilityAdmission(ctx, "invalidated")
			}
			continue
		}
		candidatesEvaluated++
		used := usage[record.NodeID]
		fit := a.policy.EvaluateFit(allocatableFromSummary(record.Summary), used.resources, nodeRequested)
		slots := evaluateRuntimeSlots(record.Summary, used.allocationIDs)
		if !fit.Fits() || !slots.Fits {
			diagnostics.AddCandidate(record.NodeID, a.policy, fit, slots)
			continue
		}
		refreshed := refreshPlacementCandidate(&placementkernel.Candidate{Record: record, Evaluation: freshEvaluation, BaseRequest: baseRequest, Request: freshRequest}, record, used.resources, used.allocationIDs, lockedEvaluationTime)
		if selected == nil || placementkernel.CandidateLess(refreshed, selected) {
			selected = refreshed
		}
	}
	if selected != nil {
		if len(selected.Request.GetCapabilityRequirements()) > 0 {
			observationResult := "unchanged"
			if !sameNodeObservationOrder(selected.Record.Summary, candidateSummary(req.Candidates, selected.NodeID)) {
				observationResult = "refreshed"
			}
			recordCapabilityAdmission(ctx, observationResult)
		}
		requirements, err := nodecapability.ResolveRequirements(selected.Record.Summary.GetCapabilitySnapshot(), selected.Request.GetCapabilityRequirements(), lockedEvaluationTime)
		if err != nil {
			recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, err)
			return nil, fmt.Errorf("resolve capability requirements: %w", err)
		}
		recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, nil)
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeCapacity, string(quotaAdmissionAllowed), "fits")
		return &placementkernel.AdmissionDecision{
			Record:                 selected.Record,
			Evaluation:             selected.Evaluation,
			Request:                selected.Request,
			CapabilityRequirements: requirements,
		}, nil
	}
	if rejection := lockedAdmissionEligibilityError(candidatesEvaluated, lockedRejectionRequest, lockedEligibilityRejections); rejection != nil {
		recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
		return nil, rejection
	}
	rejection := admissionRejectionError(diagnostics)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
	recordNodeCapacityRejected(ctx, namespace, diagnostics)
	return nil, rejection
}

func lockedAdmissionEligibilityError(candidatesEvaluated int, request *placementkernel.Request, rejected []*placementkernel.Evaluation) error {
	if candidatesEvaluated > 0 || len(rejected) == 0 {
		return nil
	}
	return placementkernel.NoEligibleNodeError(request, rejected)
}

func candidateSummary(candidates []*placementkernel.Candidate, nodeID string) *nodev1.NodeSummary {
	for _, candidate := range candidates {
		if candidate != nil && candidate.Record != nil && candidate.NodeID == nodeID {
			return candidate.Record.Summary
		}
	}
	return nil
}

func sameNodeObservationOrder(left, right *nodev1.NodeSummary) bool {
	return left != nil && right != nil && left.GetNodeInstanceID() == right.GetNodeInstanceID() && left.GetSequence() == right.GetSequence()
}

func admissionRejectionError(diagnostics admissionRejectionDiagnostics) error {
	st := grpcstatus.New(codes.ResourceExhausted, diagnostics.Message())
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   string(resourcekernel.AdmissionRejectionNodeCapacity),
		Domain:   resourcekernel.AdmissionErrorDomain,
		Metadata: diagnostics.Metadata(),
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func activeNamespaceAllocationUsage(ctx context.Context, tx pgx.Tx, namespace string) (resourcekernel.Claim, error) {
	var used resourcekernel.Claim
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(a.cpu_request_milli), 0), COALESCE(SUM(a.sandbox_memory_request_bytes), 0), COALESCE(SUM(a.ephemeral_storage_request_bytes), 0)
		FROM allocations a
		JOIN runs r ON r.run_id = a.run_id
		WHERE r.namespace = $1 AND a.lifecycle_state <> $2
	`, namespace, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()).Scan(&used.CPUMilli, &used.MemoryBytes, &used.EphemeralStorageBytes); err != nil {
		return resourcekernel.Claim{}, fmt.Errorf("sum namespace allocation charges: %w", err)
	}
	return used, nil
}
