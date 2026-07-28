package pgfunction

import (
	"context"
	"fmt"
	"strings"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) listEvents(ctx context.Context, functionID, invocationID, revisionID string, limit int32) ([]*functionv1.FunctionEvent, error) {
	query := `SELECT event_id, function_id, invocation_id, revision_id, event_type, message, details, created_at FROM function_events`
	where := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if functionID = strings.TrimSpace(functionID); functionID != "" {
		args = append(args, functionID)
		where = append(where, fmt.Sprintf("function_id = $%d", len(args)))
	}
	if invocationID = strings.TrimSpace(invocationID); invocationID != "" {
		args = append(args, invocationID)
		where = append(where, fmt.Sprintf("invocation_id = $%d", len(args)))
	}
	if revisionID = strings.TrimSpace(revisionID); revisionID != "" {
		args = append(args, revisionID)
		where = append(where, fmt.Sprintf("revision_id = $%d", len(args)))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, functionkernel.NormalizeFunctionEventLimit(limit))
	query += fmt.Sprintf(" ORDER BY event_sequence DESC LIMIT $%d", len(args))
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query function events: %w", err)
	}
	defer rows.Close()
	out := make([]*functionv1.FunctionEvent, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func recordFunctionEventTx(ctx context.Context, execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, event *functionv1.FunctionEvent) error {
	if event == nil || strings.TrimSpace(event.GetFunctionID()) == "" {
		return nil
	}
	detailsJSON, err := marshalJSONMap(event.GetDetails())
	if err != nil {
		return err
	}
	if _, err := execer.Exec(ctx, `
		INSERT INTO function_events (event_id, function_id, invocation_id, revision_id, event_type, message, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
	`, event.GetID(), event.GetFunctionID(), event.GetInvocationID(), event.GetRevisionID(), event.GetType().String(), event.GetMessage(), detailsJSON, event.GetCreatedAt().AsTime().UTC()); err != nil {
		return fmt.Errorf("record function event: %w", err)
	}
	return nil
}
