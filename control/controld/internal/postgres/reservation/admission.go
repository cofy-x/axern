package reservation

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	"github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
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

const maxReservationRejectionDetails = 5

type ReserveCandidateRequest struct {
	Namespace     string
	RunID         string
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

func (a Admission) ReserveCandidate(ctx context.Context, tx pgx.Tx, req ReserveCandidateRequest) (_ *placementkernel.AdmissionDecision, retErr error) {
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
	namespaceUsed, err := activeNamespaceReservationUsage(ctx, tx, namespace)
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
			RunID:         req.RunID,
			EnvironmentID: req.EnvironmentID,
			Evaluation:    quotaEvaluation,
			Message:       quotaRejectionMessage(namespace, quotaEvaluation),
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
	usage, err := activeCandidateReservationUsage(ctx, tx, locked)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageLoadReservations, stageStarted, err)
	if err != nil {
		return nil, err
	}
	// req.Now is the selection clock. Advance it by the real time spent waiting
	// for quota and node row locks so a short-lived observation cannot remain
	// eligible merely because admission was queued behind another transaction.
	lockedEvaluationTime := req.Now.Add(time.Since(totalStarted))
	stageStarted = time.Now()
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	lockedEligibilityRejections := make([]*placementkernel.Evaluation, 0)
	var lockedRejectionRequest *placementkernel.Request
	reservationEvaluated := 0
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
				!sameCapabilityObservationOrder(candidate.Record.Summary.GetCapabilitySnapshot(), record.Summary.GetCapabilitySnapshot()) {
				recordCapabilityAdmission(ctx, "invalidated")
			}
			continue
		}
		reservationEvaluated++
		used := usage[record.NodeID]
		effectiveUsed := effectiveReservationUsage(record.Summary, used.resources)
		fit := a.policy.EvaluateFit(allocatableFromSummary(record.Summary), effectiveUsed, nodeRequested)
		slots := evaluateRuntimeSlots(record.Summary, used.allocationIDs)
		if !fit.Fits() || !slots.Fits {
			diagnostics.AddCandidate(record.NodeID, a.policy, fit, slots)
			continue
		}
		refreshed := refreshPlacementCandidate(&placementkernel.Candidate{Record: record, Evaluation: freshEvaluation, BaseRequest: baseRequest, Request: freshRequest}, record, effectiveUsed, used.allocationIDs, lockedEvaluationTime)
		if selected == nil || placementkernel.CandidateLess(refreshed, selected) {
			selected = refreshed
		}
	}
	if selected != nil {
		if len(selected.Request.GetCapabilityRequirements()) > 0 {
			observationResult := "unchanged"
			if !sameCapabilityObservationOrder(selected.Record.Summary.GetCapabilitySnapshot(), candidateSnapshot(req.Candidates, selected.NodeID)) {
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
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeReservation, string(quotaAdmissionAllowed), "fits")
		return &placementkernel.AdmissionDecision{
			Record:                 selected.Record,
			Evaluation:             selected.Evaluation,
			Request:                selected.Request,
			CapabilityRequirements: requirements,
		}, nil
	}
	if rejection := lockedAdmissionEligibilityError(reservationEvaluated, lockedRejectionRequest, lockedEligibilityRejections); rejection != nil {
		recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
		return nil, rejection
	}
	rejection := reservationRejectionError(diagnostics)
	recordResourceAdmissionStage(ctx, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
	recordNodeReservationRejected(ctx, namespace, diagnostics)
	return nil, rejection
}

func lockedAdmissionEligibilityError(reservationEvaluated int, request *placementkernel.Request, rejected []*placementkernel.Evaluation) error {
	if reservationEvaluated > 0 || len(rejected) == 0 {
		return nil
	}
	return placementkernel.NoEligibleNodeError(request, rejected)
}

func candidateSnapshot(candidates []*placementkernel.Candidate, nodeID string) *capabilityv1.CapabilitySnapshot {
	for _, candidate := range candidates {
		if candidate != nil && candidate.Record != nil && candidate.NodeID == nodeID {
			return candidate.Record.Summary.GetCapabilitySnapshot()
		}
	}
	return nil
}

func sameCapabilityObservationOrder(left, right *capabilityv1.CapabilitySnapshot) bool {
	return left != nil && right != nil && left.GetNodeInstanceID() == right.GetNodeInstanceID() && left.GetSequence() == right.GetSequence()
}

func reservationRejectionError(diagnostics reservationRejectionDiagnostics) error {
	st := grpcstatus.New(codes.ResourceExhausted, diagnostics.Message())
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   string(resourcekernel.AdmissionRejectionNodeReservationCapacity),
		Domain:   resourcekernel.AdmissionErrorDomain,
		Metadata: diagnostics.Metadata(),
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func activeNamespaceReservationUsage(ctx context.Context, tx pgx.Tx, namespace string) (resourcekernel.Claim, error) {
	var used resourcekernel.Claim
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(res.cpu_milli), 0), COALESCE(SUM(res.sandbox_memory_request_bytes), 0), COALESCE(SUM(res.ephemeral_storage_bytes), 0)
		FROM reservations res
		JOIN allocations a ON a.allocation_id = res.allocation_id
		JOIN runs r ON r.run_id = a.run_id
		WHERE r.namespace = $1 AND res.released_at IS NULL
	`, namespace).Scan(&used.CPUMilli, &used.MemoryBytes, &used.EphemeralStorageBytes); err != nil {
		return resourcekernel.Claim{}, fmt.Errorf("sum namespace reservations: %w", err)
	}
	return used, nil
}
