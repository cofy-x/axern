package appservice

import (
	"context"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func (c *controller) reportFailure(ctx context.Context, current *servicev1.Service, replicaID, message string, phase servicev1.ServiceRolloutPhase, replacementBlocked bool, now time.Time) (*servicev1.Service, error) {
	if current == nil {
		return nil, nil
	}
	beforeStatus := current.GetStatus()
	if strings.TrimSpace(replicaID) == "" {
		next, err := c.markDegraded(current.GetID(), message, now)
		if err != nil {
			return current, err
		}
		current = next
	} else {
		current = c.markAllocationCreateFailed(current, replicaID, message, now)
	}
	if current == nil {
		return nil, nil
	}
	diagnostic := workloadkernel.ClassifyDiagnostic(commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, message)
	if replacementBlocked {
		if err := c.recordEvent(ctx, servicekernel.NewServiceEvent(
			current.GetID(),
			replicaID,
			servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED,
			phase,
			diagnostic,
			message,
			now,
		)); err != nil {
			return current, err
		}
	}
	if err := c.emitServiceDegradedTransition(ctx, beforeStatus, current, replicaID, phase, message, diagnostic, now); err != nil {
		return current, err
	}
	return current, nil
}

func (c *controller) emitServiceDegradedTransition(ctx context.Context, beforeStatus servicev1.ServiceStatus, current *servicev1.Service, replicaID string, phase servicev1.ServiceRolloutPhase, message string, diagnostic commonv1.WorkloadDiagnosticCode, now time.Time) error {
	if current == nil {
		return nil
	}
	if beforeStatus == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED || current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		return nil
	}
	return c.recordEvent(ctx, servicekernel.NewServiceEvent(
		current.GetID(),
		replicaID,
		servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED,
		phase,
		diagnostic,
		message,
		now,
	))
}

func (c *controller) recordEvent(ctx context.Context, event *servicev1.ServiceEvent) error {
	if c.events == nil || event == nil {
		return nil
	}
	ctx, span := sdkobs.Start(ctx, ctrlobs.SpanServiceEvent,
		attribute.String(sdkobs.AttrServiceID, event.GetServiceID()),
		attribute.String(sdkobs.AttrAllocationID, event.GetReplicaID()),
		attribute.String(sdkobs.AttrServiceEventType, event.GetType().String()),
		attribute.String(sdkobs.AttrDiagnosticCode, event.GetDiagnosticCode().String()),
	)
	defer span.End()
	if err := c.events.RecordEvent(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "record service event")
		return err
	}
	span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"))
	return nil
}
