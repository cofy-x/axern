package pgadmin

import (
	"context"
	"fmt"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type adminAuditQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Store) ListAdminAuditEvents(ctx context.Context, filter adminkernel.AuditEventFilter) ([]adminkernel.AuditEvent, error) {
	return listAdminAuditEvents(ctx, s.db.Pool(), filter)
}

func (s *Store) RecordAdminAuditEvent(ctx context.Context, event adminkernel.AuditEvent) error {
	event.Operation = strings.TrimSpace(event.Operation)
	event.TargetType = strings.TrimSpace(event.TargetType)
	event.TargetID = strings.TrimSpace(event.TargetID)
	event.OperatorReason = strings.TrimSpace(event.OperatorReason)
	if event.EventID == "" {
		event.EventID = "admaudit-" + uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := adminkernel.ValidateAuditEventFilter(adminkernel.AuditEventFilter{
		Operation:  event.Operation,
		TargetType: event.TargetType,
		TargetID:   event.TargetID,
		Limit:      1,
	}); err != nil {
		return err
	}
	if event.TargetID == "" {
		return fmt.Errorf("admin audit target id is required")
	}
	if strings.TrimSpace(event.OperatorReason) == "" {
		return fmt.Errorf("operator reason is required")
	}
	_, err := s.db.Pool().Exec(ctx, `
		INSERT INTO admin_audit_events (
			event_id, operation, target_type, target_id, operator_reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, event.EventID, event.Operation, event.TargetType, event.TargetID, event.OperatorReason, event.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert admin audit event: %w", err)
	}
	return nil
}

func insertAdminAuditEvent(ctx context.Context, tx pgx.Tx, event adminAuditEvent) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO admin_audit_events (
			event_id, operation, target_type, target_id, operator_reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, event.EventID, event.Operation, event.TargetType, event.TargetID, event.OperatorReason, event.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("insert admin audit event: %w", err)
	}
	return nil
}

func listAdminAuditEvents(ctx context.Context, queryer adminAuditQueryer, filter adminkernel.AuditEventFilter) ([]adminkernel.AuditEvent, error) {
	filter = adminkernel.NormalizeAuditEventFilter(filter)
	if err := adminkernel.ValidateAuditEventFilter(filter); err != nil {
		return nil, err
	}
	query := `
		SELECT event_id, operation, target_type, target_id, operator_reason, created_at
		FROM admin_audit_events
	`
	where := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if filter.Operation != "" {
		args = append(args, filter.Operation)
		where = append(where, fmt.Sprintf("operation = $%d", len(args)))
	}
	if filter.TargetType != "" {
		args = append(args, filter.TargetType)
		where = append(where, fmt.Sprintf("target_type = $%d", len(args)))
	}
	if filter.TargetID != "" {
		args = append(args, filter.TargetID)
		where = append(where, fmt.Sprintf("target_id = $%d", len(args)))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, filter.Limit)
	query += fmt.Sprintf(`
		ORDER BY created_at DESC, event_id DESC
		LIMIT $%d
	`, len(args))
	rows, err := queryer.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query admin audit events: %w", err)
	}
	defer rows.Close()
	out := make([]adminkernel.AuditEvent, 0)
	for rows.Next() {
		var event adminkernel.AuditEvent
		if err := rows.Scan(
			&event.EventID,
			&event.Operation,
			&event.TargetType,
			&event.TargetID,
			&event.OperatorReason,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
