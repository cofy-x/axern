package appadmin

import (
	"context"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
)

type AuditStore interface {
	ListAdminAuditEvents(ctx context.Context, filter adminkernel.AuditEventFilter) ([]adminkernel.AuditEvent, error)
}

type AuditControl struct {
	store AuditStore
}

func NewAuditControl(store AuditStore) AuditControl {
	return AuditControl{store: store}
}

func (c AuditControl) ListAdminAuditEvents(ctx context.Context, filter adminkernel.AuditEventFilter) ([]adminkernel.AuditEvent, error) {
	filter = adminkernel.NormalizeAuditEventFilter(filter)
	if err := adminkernel.ValidateAuditEventFilter(filter); err != nil {
		return nil, err
	}
	return c.store.ListAdminAuditEvents(ctx, filter)
}
