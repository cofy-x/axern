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
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
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
	OwnerType     string
	OwnerID       string
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
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageTotal, totalStarted, retErr)
	}()
	namespace := environmentkernel.NormalizeNamespace(req.Namespace)
	if a.placement == nil {
		return nil, fmt.Errorf("placement evaluator is required for durable admission")
	}
	requested := resourcekernel.QuantityToClaim(executionkernel.NormalizeConfig(req.Config).GetResources().GetRequests())
	nodeRequested := requested
	nodeRequested.MemoryBytes = resourcekernel.SaturatingAdd(nodeRequested.MemoryBytes, a.policy.RuntimeMemoryOverhead(req.Config.GetRuntimeClass()))
	stageStarted := time.Now()
	quota, err := pgnamespace.LockQuotaPolicy(ctx, tx, namespace)
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageLockNamespace, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	namespaceUsed, err := activeNamespaceReservationUsage(ctx, tx, namespace)
	if err != nil {
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageEvaluateNamespace, stageStarted, err)
		return nil, err
	}
	quotaEvaluation := quota.EvaluateFit(namespaceUsed, requested)
	recordQuotaEvaluation(ctx, namespace, quotaEvaluation)
	if !quotaEvaluation.Fits() {
		rejection := quotaRejectionError(namespace, quotaEvaluation)
		if err := insertQuotaAdmissionRejectedEvent(ctx, tx, quotaAdmissionRejectedEvent{
			Namespace:     namespace,
			OwnerType:     req.OwnerType,
			OwnerID:       req.OwnerID,
			EnvironmentID: req.EnvironmentID,
			Evaluation:    quotaEvaluation,
			Message:       quotaRejectionMessage(namespace, quotaEvaluation),
			CreatedAt:     req.Now,
		}); err != nil {
			recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageEvaluateNamespace, stageStarted, err)
			return nil, err
		}
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageEvaluateNamespace, stageStarted, rejection)
		return nil, committedAdmissionError{err: rejection}
	}
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageEvaluateNamespace, stageStarted, nil)
	stageStarted = time.Now()
	locked, err := lockCandidateNodes(ctx, tx, req.Candidates)
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageLockCandidates, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	usage, err := activeCandidateReservationUsage(ctx, tx, locked)
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageLoadReservations, stageStarted, err)
	if err != nil {
		return nil, err
	}
	// req.Now is the selection clock. Advance it by the real time spent waiting
	// for quota and node row locks so a short-lived observation cannot remain
	// eligible merely because admission was queued behind another transaction.
	lockedEvaluationTime := req.Now.Add(time.Since(totalStarted))
	stageStarted = time.Now()
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	lockedEligibilityRejections := make([]*nodev1.PlacementCandidate, 0)
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
		if freshEvaluation == nil || freshEvaluation.GetState() != nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_ELIGIBLE {
			if freshEvaluation != nil {
				lockedEligibilityRejections = append(lockedEligibilityRejections, freshEvaluation)
				if lockedRejectionRequest == nil {
					lockedRejectionRequest = freshRequest
				}
			}
			if len(freshRequest.GetCapabilityRequirements()) > 0 &&
				candidate.Record.Summary.GetCapabilitySnapshot().GetSnapshotID() != record.Summary.GetCapabilitySnapshot().GetSnapshotID() {
				recordCapabilityAdmissionEvidence(ctx, "invalidated")
			}
			continue
		}
		reservationEvaluated++
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
			evidenceResult := "unchanged"
			if selected.Record.Summary.GetCapabilitySnapshot().GetSnapshotID() != candidateSnapshotID(req.Candidates, selected.NodeID) {
				evidenceResult = "refreshed"
			}
			recordCapabilityAdmissionEvidence(ctx, evidenceResult)
		}
		dependencies, err := nodecapability.ResolveDependencies(selected.Record.Summary.GetCapabilitySnapshot(), selected.Request.GetCapabilityRequirements(), lockedEvaluationTime)
		if err != nil {
			recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, err)
			return nil, fmt.Errorf("resolve admitted capability evidence: %w", err)
		}
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, nil)
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeReservation, string(quotaAdmissionAllowed), "fits")
		return &placementkernel.AdmissionDecision{
			Record:                 selected.Record,
			Evaluation:             selected.Evaluation,
			Request:                selected.Request,
			CapabilityDependencies: dependencies,
		}, nil
	}
	if rejection := lockedAdmissionEligibilityError(reservationEvaluated, lockedRejectionRequest, lockedEligibilityRejections); rejection != nil {
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
		return nil, rejection
	}
	rejection := reservationRejectionError(diagnostics)
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
	recordNodeReservationRejected(ctx, namespace, diagnostics)
	return nil, rejection
}

func lockedAdmissionEligibilityError(reservationEvaluated int, request *placementkernel.Request, rejected []*nodev1.PlacementCandidate) error {
	if reservationEvaluated > 0 || len(rejected) == 0 {
		return nil
	}
	return placementkernel.NoEligibleNodeError(request, rejected)
}

func candidateSnapshotID(candidates []*placementkernel.Candidate, nodeID string) string {
	for _, candidate := range candidates {
		if candidate != nil && candidate.Record != nil && candidate.NodeID == nodeID {
			return candidate.Record.Summary.GetCapabilitySnapshot().GetSnapshotID()
		}
	}
	return ""
}

func (a Admission) RuntimeMemoryOverhead(runtimeName string) int64 {
	return a.policy.RuntimeMemoryOverhead(runtimeName)
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
		SELECT COALESCE(SUM(cpu_milli), 0), COALESCE(SUM(memory_bytes), 0), COALESCE(SUM(ephemeral_storage_bytes), 0)
		FROM workload_reservations
		WHERE namespace = $1 AND released_at IS NULL
	`, namespace).Scan(&used.CPUMilli, &used.MemoryBytes, &used.EphemeralStorageBytes); err != nil {
		return resourcekernel.Claim{}, fmt.Errorf("sum namespace reservations: %w", err)
	}
	return used, nil
}
