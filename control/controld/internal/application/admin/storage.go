package appadmin

import (
	"context"
	"fmt"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
)

type StorageBindingStore interface {
	ListStorageBindings(ctx context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error)
	ListStorageReclaims(ctx context.Context, filter adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error)
	RetryStorageBinding(ctx context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error)
}

func (c StorageControl) ListStorageReclaims(ctx context.Context, filter adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error) {
	if c.store == nil {
		return nil, fmt.Errorf("storage admin store is required")
	}
	return c.store.ListStorageReclaims(ctx, adminkernel.NormalizeStorageReclaimFilter(filter))
}

type StorageAuditRecorder interface {
	RecordAdminAuditEvent(ctx context.Context, event adminkernel.AuditEvent) error
}

type StorageControl struct {
	store  StorageBindingStore
	audits StorageAuditRecorder
	now    func() time.Time
}

func NewStorageControl(store StorageBindingStore, audits StorageAuditRecorder, now func() time.Time) StorageControl {
	return StorageControl{store: store, audits: audits, now: now}
}

func (c StorageControl) ListStorageBindings(ctx context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error) {
	if c.store == nil {
		return nil, fmt.Errorf("storage admin store is required")
	}
	filter = adminkernel.NormalizeStorageBindingFilter(filter)
	if err := adminkernel.ValidateStorageBindingFilter(filter); err != nil {
		return nil, err
	}
	return c.store.ListStorageBindings(ctx, filter)
}

func (c StorageControl) RetryStorageBinding(ctx context.Context, req adminkernel.RetryStorageBindingRequest) (*adminkernel.StorageBinding, error) {
	if c.store == nil {
		return nil, fmt.Errorf("storage admin store is required")
	}
	req = adminkernel.NormalizeRetryStorageBindingRequest(req)
	if err := adminkernel.ValidateRetryStorageBindingRequest(req); err != nil {
		return nil, err
	}
	if c.audits == nil {
		return nil, fmt.Errorf("storage admin audit recorder is required")
	}
	if err := c.audits.RecordAdminAuditEvent(ctx, adminkernel.AuditEvent{
		Operation:      adminkernel.AuditOperationRetryStorageBinding,
		TargetType:     adminkernel.AuditTargetStorageBinding,
		TargetID:       req.BindingID,
		OperatorReason: req.OperatorReason,
		CreatedAt:      c.timestamp(),
	}); err != nil {
		return nil, err
	}
	return c.store.RetryStorageBinding(ctx, req)
}

func (c StorageControl) timestamp() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now().UTC()
}
