package pgrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/control/controld/internal/kernel/pagecursor"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) GetRun(ctx context.Context, id string) (*runv1.Run, error) {
	run, err := scanRun(s.db.Pool().QueryRow(ctx, runSelectSQL()+` WHERE r.run_id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "run %q not found", id)
	}
	return run, err
}

func (s *Store) WatchRun(ctx context.Context, id string, afterVersion int64) (*runv1.Run, error) {
	id = strings.TrimSpace(id)
	subscription, err := s.runWatches.subscribe(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("subscribe run changes: %w", err)
	}
	defer subscription.close()
	for {
		run, err := s.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		if run.GetVersion() > afterVersion || runkernel.IsTerminal(run.GetStatus()) {
			return run, nil
		}
		if err := subscription.wait(ctx); err != nil {
			return nil, err
		}
	}
}

func (s *Store) ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, string, error) {
	if filter == nil {
		filter = &runv1.RunListFilter{}
	}
	cursor, err := pagecursor.Decode(filter.GetCursor())
	if err != nil {
		return nil, "", err
	}
	query := runSelectSQL() + ` WHERE TRUE`
	args := make([]any, 0, 6)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" {
		query += ` AND r.namespace = ` + add(namespace)
	}
	if len(filter.GetStatuses()) > 0 {
		statuses := make([]string, 0, len(filter.GetStatuses()))
		for _, status := range filter.GetStatuses() {
			statuses = append(statuses, status.String())
		}
		query += ` AND r.status = ANY(` + add(statuses) + `)`
	}
	if len(filter.GetLabels()) > 0 {
		labels, marshalErr := json.Marshal(filter.GetLabels())
		if marshalErr != nil {
			return nil, "", fmt.Errorf("marshal run label filter: %w", marshalErr)
		}
		query += ` AND r.labels @> ` + add(string(labels)) + `::jsonb`
	}
	if !cursor.CreatedAt.IsZero() {
		query += ` AND (r.created_at, r.run_id) < (` + add(cursor.CreatedAt) + `, ` + add(cursor.ID) + `)`
	}
	pageSize := pagecursor.PageSize(filter.GetPageSize())
	query += ` ORDER BY r.created_at DESC, r.run_id DESC LIMIT ` + add(pageSize+1)
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()
	out := make([]*runv1.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(out) > pageSize {
		out = out[:pageSize]
		last := out[len(out)-1]
		nextCursor = pagecursor.Encode(last.GetCreatedAt().AsTime(), last.GetID())
	}
	return out, nextCursor, nil
}
