package pgrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/control/controld/internal/kernel/pagecursor"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error) {
	row := s.db.Pool().QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1`, strings.TrimSpace(id))
	env, err := scanEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "environment %q not found", id)
	}
	return env, err
}

func (s *Store) ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, string, error) {
	if filter == nil {
		filter = &environmentv1.ListFilter{}
	}
	cursor, err := pagecursor.Decode(filter.GetCursor())
	if err != nil {
		return nil, "", err
	}
	query := environmentSelectSQL() + ` WHERE TRUE`
	args := make([]any, 0, 4)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" {
		query += ` AND namespace = ` + add(namespace)
	}
	if len(filter.GetLabels()) > 0 {
		labels, marshalErr := json.Marshal(filter.GetLabels())
		if marshalErr != nil {
			return nil, "", fmt.Errorf("marshal environment label filter: %w", marshalErr)
		}
		query += ` AND labels @> ` + add(string(labels)) + `::jsonb`
	}
	if !cursor.CreatedAt.IsZero() {
		query += ` AND (created_at, environment_id) < (` + add(cursor.CreatedAt) + `, ` + add(cursor.ID) + `)`
	}
	pageSize := pagecursor.PageSize(filter.GetPageSize())
	query += ` ORDER BY created_at DESC, environment_id DESC LIMIT ` + add(pageSize+1)
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query environments: %w", err)
	}
	defer rows.Close()
	out := make([]*environmentv1.Environment, 0)
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, env)
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
