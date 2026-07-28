package pgservice

import (
	"context"
	"fmt"
	"strings"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) enrichService(ctx context.Context, service *servicev1.Service) error {
	if service == nil {
		return nil
	}
	allocations, err := s.CurrentServiceAllocations(ctx, service.GetID())
	if err != nil {
		return fmt.Errorf("load rollout allocations for service %q: %w", service.GetID(), err)
	}
	message, err := s.serviceLifecycleRetryMessage(ctx, service.GetID())
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) != "" {
		service.Message = message
	}
	applyServiceDiagnostics(service)
	service.RolloutStatus = servicekernel.BuildRolloutStatus(service, allocations)
	return nil
}

func (s *PGStore) enrichServices(ctx context.Context, services []*servicev1.Service) error {
	if len(services) == 0 {
		return nil
	}
	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		serviceIDs = append(serviceIDs, service.GetID())
	}
	allocationsByService, err := s.listServiceAllocationsByServiceIDs(ctx, serviceIDs)
	if err != nil {
		return err
	}
	retryMessages, err := s.serviceLifecycleRetryMessages(ctx, serviceIDs)
	if err != nil {
		return err
	}
	for _, service := range services {
		if service == nil {
			continue
		}
		if retryMessage := strings.TrimSpace(retryMessages[service.GetID()]); retryMessage != "" {
			service.Message = retryMessage
		}
		applyServiceDiagnostics(service)
		service.RolloutStatus = servicekernel.BuildRolloutStatus(service, allocationsByService[service.GetID()])
	}
	return nil
}

func applyServiceDiagnostics(service *servicev1.Service) {
	if service == nil {
		return
	}
	service.DiagnosticCode = workloadkernel.ClassifyDiagnostic(serviceDiagnosticAllocationStatus(service.GetStatus()), service.GetMessage())
}

func serviceDiagnosticAllocationStatus(status servicev1.ServiceStatus) commonv1.AllocationStatus {
	switch status {
	case servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED, servicev1.ServiceStatus_SERVICE_STATUS_FAILED:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	default:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
	}
}

func (s *PGStore) serviceLifecycleRetryMessage(ctx context.Context, serviceID string) (string, error) {
	var message string
	err := s.db.Pool().QueryRow(ctx, `
		SELECT q.last_error
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE a.owner_type = $1
			AND a.owner_id = $2
			AND q.reason = $3
			AND btrim(q.last_error) <> ''
		ORDER BY q.updated_at DESC, q.next_run_at DESC, q.allocation_id DESC
		LIMIT 1
	`, allocationOwnerService, strings.TrimSpace(serviceID), allocationkernel.ReconcileReasonCreate).Scan(&message)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("load service lifecycle retry message: %w", err)
	}
	return strings.TrimSpace(message), nil
}

func (s *PGStore) serviceLifecycleRetryMessages(ctx context.Context, serviceIDs []string) (map[string]string, error) {
	serviceIDs = normalizedIDs(serviceIDs)
	out := make(map[string]string)
	if len(serviceIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT DISTINCT ON (a.owner_id) a.owner_id, q.last_error
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE a.owner_type = $1
			AND a.owner_id = ANY($2)
			AND q.reason = $3
			AND btrim(q.last_error) <> ''
		ORDER BY a.owner_id, q.updated_at DESC, q.next_run_at DESC, q.allocation_id DESC
	`, allocationOwnerService, serviceIDs, allocationkernel.ReconcileReasonCreate)
	if err != nil {
		return nil, fmt.Errorf("load service lifecycle retry messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var serviceID, message string
		if err := rows.Scan(&serviceID, &message); err != nil {
			return nil, err
		}
		out[serviceID] = strings.TrimSpace(message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
