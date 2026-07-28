package dashboard

import (
	"context"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
)

func (c Control) Admin(ctx context.Context) (AdminState, error) {
	retriesResp, err := c.retries.ListRetries(ctx, appadmin.LifecycleRetryListOptions{
		Limit: int(DefaultEventLimit),
	})
	if err != nil {
		return AdminState{}, err
	}
	auditResp, err := c.audit.ListEvents(ctx, appadmin.AuditListOptions{
		Limit: int(DefaultEventLimit),
	})
	if err != nil {
		return AdminState{}, err
	}
	out := AdminState{
		Retries: make([]AllocationLifecycleRetryDTO, 0, len(retriesResp.GetRetries())),
		Audit:   make([]AdminAuditEventDTO, 0, len(auditResp.GetEvents())),
	}
	for _, retry := range retriesResp.GetRetries() {
		out.Retries = append(out.Retries, NewAllocationLifecycleRetryDTO(retry))
	}
	for _, event := range auditResp.GetEvents() {
		out.Audit = append(out.Audit, NewAdminAuditEventDTO(event))
	}
	return out, nil
}

func (c Control) ForceAllocationLifecycleRetry(ctx context.Context, allocationID string, reason string, operatorReason string) (AdminRetryActionResult, error) {
	resp, err := c.retries.ForceRetry(ctx, allocationID, reason, operatorReason)
	if err != nil {
		return AdminRetryActionResult{}, err
	}
	return AdminRetryActionResult{Retry: NewAllocationLifecycleRetryDTO(resp.GetRetry())}, nil
}

func (c Control) FailAllocationLifecycleCreateRetry(ctx context.Context, allocationID string, operatorReason string) (AdminRetryActionResult, error) {
	resp, err := c.retries.FailCreateRetry(ctx, allocationID, operatorReason)
	if err != nil {
		return AdminRetryActionResult{}, err
	}
	return AdminRetryActionResult{Retry: NewAllocationLifecycleRetryDTO(resp.GetFailedRetry())}, nil
}

func (c Control) ClearAllocationLifecycleRetry(ctx context.Context, allocationID string, reason string, operatorReason string) (AdminRetryActionResult, error) {
	resp, err := c.retries.ClearRetry(ctx, allocationID, reason, operatorReason)
	if err != nil {
		return AdminRetryActionResult{}, err
	}
	return AdminRetryActionResult{Retry: NewAllocationLifecycleRetryDTO(resp.GetClearedRetry())}, nil
}
