package reservation

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Admission struct {
	policy resourcekernel.AdmissionPolicy
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

func NewAdmission(policy resourcekernel.AdmissionPolicy) Admission {
	return Admission{policy: resourcekernel.NormalizeAdmissionPolicy(policy)}
}

func (a Admission) ReserveCandidate(ctx context.Context, tx pgx.Tx, req ReserveCandidateRequest) (_ *nodekernel.Record, retErr error) {
	totalStarted := time.Now()
	defer func() {
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageTotal, totalStarted, retErr)
	}()
	namespace := environmentkernel.NormalizeNamespace(req.Namespace)
	requested := resourcekernel.QuantityToClaim(executionkernel.NormalizeConfig(req.Config).GetResources().GetRequests())
	nodeRequested := requested
	nodeRequested.MemoryBytes += a.policy.RuntimeMemoryOverhead(req.Config.GetRuntimeClass())
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
	stageStarted = time.Now()
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	var selected *placementkernel.Candidate
	for _, candidate := range req.Candidates {
		if candidate == nil || candidate.Record == nil {
			continue
		}
		record := locked[candidate.NodeID]
		if record == nil || !record.Active() {
			continue
		}
		used := usage[record.NodeID]
		fit := a.policy.EvaluateFit(allocatableFromSummary(record.Summary), used.resources, nodeRequested)
		slots := evaluateRuntimeSlots(record.Summary, used.allocationIDs)
		if !fit.Fits() || !slots.Fits {
			diagnostics.AddCandidate(record.NodeID, a.policy, fit, slots)
			continue
		}
		refreshed := refreshPlacementCandidate(candidate, record, used.resources, used.allocationIDs, req.Now)
		if selected == nil || placementkernel.CandidateLess(refreshed, selected) {
			selected = refreshed
		}
	}
	if selected != nil {
		recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, nil)
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeReservation, string(quotaAdmissionAllowed), "fits")
		return selected.Record, nil
	}
	rejection := reservationRejectionError(diagnostics)
	recordResourceAdmissionStage(ctx, req.OwnerType, resourceAdmissionStageSelectCandidate, stageStarted, rejection)
	recordNodeReservationRejected(ctx, namespace, diagnostics)
	return nil, rejection
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
		SELECT COALESCE(SUM(cpu_milli), 0), COALESCE(SUM(memory_bytes), 0), COALESCE(SUM(writable_layer_bytes), 0)
		FROM workload_reservations
		WHERE namespace = $1 AND released_at IS NULL
	`, namespace).Scan(&used.CPUMilli, &used.MemoryBytes, &used.WritableLayerBytes); err != nil {
		return resourcekernel.Claim{}, fmt.Errorf("sum namespace reservations: %w", err)
	}
	return used, nil
}
