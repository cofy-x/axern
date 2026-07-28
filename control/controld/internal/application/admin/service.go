package appadmin

import (
	"context"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type ServicePurger interface {
	Purge(ctx context.Context, id string, now time.Time) (string, bool, error)
}

type ServiceControl struct {
	services ServicePurger
	audits   StorageAuditRecorder
}

func NewServiceControl(services ServicePurger, audits StorageAuditRecorder) ServiceControl {
	return ServiceControl{services: services, audits: audits}
}

func (c ServiceControl) PurgeService(ctx context.Context, serviceID, operatorReason string, now time.Time) (string, error) {
	serviceID = strings.TrimSpace(serviceID)
	operatorReason = strings.TrimSpace(operatorReason)
	if serviceID == "" {
		return "", grpcstatus.Error(codes.InvalidArgument, "service_id is required")
	}
	if operatorReason == "" {
		return "", grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	if c.services == nil || c.audits == nil {
		return "", grpcstatus.Error(codes.Unavailable, "service admin is unavailable")
	}
	if err := c.audits.RecordAdminAuditEvent(ctx, adminkernel.AuditEvent{
		Operation:      adminkernel.AuditOperationPurgeService,
		TargetType:     adminkernel.AuditTargetService,
		TargetID:       serviceID,
		OperatorReason: operatorReason,
		CreatedAt:      now,
	}); err != nil {
		return "", err
	}
	purgedID, ok, err := c.services.Purge(ctx, serviceID, now)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", grpcstatus.Error(codes.NotFound, "service not found")
	}
	return purgedID, nil
}
