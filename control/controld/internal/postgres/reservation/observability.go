package reservation

import (
	"context"
	"strings"
	"time"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type quotaAdmissionResult string

const (
	quotaAdmissionAllowed  quotaAdmissionResult = "allowed"
	quotaAdmissionRejected quotaAdmissionResult = "rejected"
)

const (
	resourceAdmissionStageLockNamespace     = "lock_namespace"
	resourceAdmissionStageEvaluateNamespace = "evaluate_namespace"
	resourceAdmissionStageLockCandidates    = "lock_candidates"
	resourceAdmissionStageLoadReservations  = "load_reservations"
	resourceAdmissionStageSelectCandidate   = "select_candidate"
	resourceAdmissionStageTotal             = "total"
)

type quotaAdmissionReason string

const (
	quotaAdmissionReasonFits               quotaAdmissionReason = "fits"
	quotaAdmissionReasonExceeded           quotaAdmissionReason = "exceeded"
	quotaAdmissionReasonInsufficientCPU    quotaAdmissionReason = "insufficient_cpu"
	quotaAdmissionReasonInsufficientMemory quotaAdmissionReason = "insufficient_memory"
)

type resourceAdmissionScope string

const (
	resourceAdmissionScopeQuota           resourceAdmissionScope = "namespace_quota"
	resourceAdmissionScopeNodeReservation resourceAdmissionScope = "node_reservation"
)

func recordQuotaAdmission(ctx context.Context, namespace string, result quotaAdmissionResult, reason quotaAdmissionReason) {
	counter := sdkobs.Int64Counter(ctrlobs.MetricQuotaAdmissionTotal.Name, ctrlobs.MetricQuotaAdmissionTotal.Description)
	counter.Add(ctx, 1,
		attribute.String(sdkobs.AttrNamespace, namespace),
		attribute.String(sdkobs.AttrResult, string(result)),
		attribute.String(sdkobs.AttrReason, string(reason)),
	)
	recordResourceAdmission(ctx, namespace, resourceAdmissionScopeQuota, string(result), string(reason))
}

func recordResourceAdmission(ctx context.Context, namespace string, scope resourceAdmissionScope, result string, reason string) {
	counter := sdkobs.Int64Counter(ctrlobs.MetricResourceAdmissionTotal.Name, ctrlobs.MetricResourceAdmissionTotal.Description)
	counter.Add(ctx, 1,
		attribute.String(sdkobs.AttrNamespace, namespace),
		attribute.String("scope", string(scope)),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrReason, reason),
	)
}

func recordResourceAdmissionStage(ctx context.Context, ownerType, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		code := grpcstatus.Code(err)
		if code != codes.OK && code != codes.Unknown {
			errorClass = strings.ToLower(code.String())
		} else {
			errorClass = "error"
		}
	}
	sdkobs.DurationHistogram(ctrlobs.MetricResourceAdmissionStageDuration.Name, ctrlobs.MetricResourceAdmissionStageDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrOwnerType, ownerType),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func recordQuotaEvaluation(ctx context.Context, namespace string, evaluation resourcekernel.QuotaEvaluation) {
	if evaluation.Fits() {
		recordQuotaAdmission(ctx, namespace, quotaAdmissionAllowed, quotaAdmissionReasonFits)
		return
	}
	recorded := false
	if evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits {
		recordQuotaAdmission(ctx, namespace, quotaAdmissionRejected, quotaAdmissionReasonInsufficientCPU)
		recorded = true
	}
	if evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits {
		recordQuotaAdmission(ctx, namespace, quotaAdmissionRejected, quotaAdmissionReasonInsufficientMemory)
		recorded = true
	}
	if !recorded {
		recordQuotaAdmission(ctx, namespace, quotaAdmissionRejected, quotaAdmissionReasonExceeded)
	}
}

func recordNodeReservationRejected(ctx context.Context, namespace string, diagnostics reservationRejectionDiagnostics) {
	resources := diagnostics.rejectedResources()
	if len(resources) == 0 {
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeReservation, string(quotaAdmissionRejected), "exceeded")
		return
	}
	for _, resource := range resources {
		recordResourceAdmission(ctx, namespace, resourceAdmissionScopeNodeReservation, string(quotaAdmissionRejected), "insufficient_"+resource)
	}
}
