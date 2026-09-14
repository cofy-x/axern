package namespace

import (
	"context"
	"errors"
	"fmt"
	"time"

	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) CreateNamespace(ctx context.Context, namespace string, now time.Time) (*namespacev1.Namespace, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	normalized, err := EnsureAt(ctx, tx, namespace, now)
	if err != nil {
		return nil, err
	}
	record, err := queryNamespace(ctx, tx, normalized)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) GetNamespace(ctx context.Context, namespace string) (*namespacev1.Namespace, error) {
	normalized := normalizeNamespace(namespace)
	record, err := queryNamespace(ctx, s.db.Pool(), normalized)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) ListNamespaces(ctx context.Context) ([]*namespacev1.Namespace, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT namespace, created_at, deleted_at
		FROM namespaces
		WHERE deleted_at IS NULL
		ORDER BY namespace
	`)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	defer rows.Close()
	var namespaces []*namespacev1.Namespace
	for rows.Next() {
		record, err := scanNamespace(rows)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return namespaces, nil
}

func (s *Store) DeleteNamespace(ctx context.Context, namespace string, now time.Time) (*namespacev1.Namespace, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	normalized := normalizeNamespace(namespace)
	record, err := queryNamespaceForUpdate(ctx, tx, normalized)
	if err != nil {
		return nil, err
	}
	if err := ensureNamespaceDeletable(ctx, tx, normalized); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE namespaces SET deleted_at = $2 WHERE namespace = $1`, normalized, now.UTC()); err != nil {
		return nil, fmt.Errorf("delete namespace: %w", err)
	}
	record.DeletedAt = timestamppb.New(now.UTC())
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return record, nil
}

func EnsureAt(ctx context.Context, q pgx.Tx, namespace string, now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	normalized := normalizeNamespace(namespace)
	if _, err := q.Exec(ctx, `
		INSERT INTO namespaces(namespace, created_at)
		VALUES ($1, $2)
		ON CONFLICT (namespace) DO NOTHING
	`, normalized, now); err != nil {
		return "", fmt.Errorf("ensure namespace: %w", err)
	}
	var deletedAt *time.Time
	if err := q.QueryRow(ctx, `SELECT deleted_at FROM namespaces WHERE namespace = $1 FOR UPDATE`, normalized).Scan(&deletedAt); err != nil {
		return "", fmt.Errorf("verify namespace: %w", err)
	}
	if deletedAt != nil {
		return "", grpcstatus.Errorf(codes.FailedPrecondition, "namespace %q was deleted and cannot be reused", normalized)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO namespace_resource_quotas(namespace, created_at, updated_at)
		VALUES ($1, $2, $2)
		ON CONFLICT (namespace) DO NOTHING
	`, normalized, now); err != nil {
		return "", fmt.Errorf("ensure namespace quota: %w", err)
	}
	return normalized, nil
}

func queryNamespace(ctx context.Context, q queryer, namespace string) (*namespacev1.Namespace, error) {
	row := q.QueryRow(ctx, `
		SELECT namespace, created_at, deleted_at
		FROM namespaces
		WHERE namespace = $1 AND deleted_at IS NULL
	`, namespace)
	record, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "namespace %q not found", namespace)
	}
	if err != nil {
		return nil, fmt.Errorf("get namespace: %w", err)
	}
	return record, nil
}

func queryNamespaceForUpdate(ctx context.Context, q queryer, namespace string) (*namespacev1.Namespace, error) {
	row := q.QueryRow(ctx, `
		SELECT namespace, created_at, deleted_at
		FROM namespaces
		WHERE namespace = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, namespace)
	record, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "namespace %q not found", namespace)
	}
	if err != nil {
		return nil, fmt.Errorf("get namespace: %w", err)
	}
	return record, nil
}

func ensureNamespaceDeletable(ctx context.Context, q queryer, namespace string) error {
	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "resource-owning allocations",
			query: `SELECT EXISTS (
				SELECT 1
				FROM allocations a
				JOIN runs r ON r.run_id = a.run_id
				WHERE r.namespace = $1 AND a.lifecycle_state <> 'ALLOCATION_LIFECYCLE_STATE_RELEASED'
			)`,
		},
		{
			name: "active role bindings",
			query: `SELECT EXISTS (
				SELECT 1 FROM role_bindings
				WHERE namespace = $1 AND revoked_at IS NULL
			)`,
		},
		{
			name: "active runs",
			query: `SELECT EXISTS (
				SELECT 1 FROM runs
				WHERE namespace = $1
				  AND status NOT IN ($2, $3, $4)
			)`,
			args: []any{
				runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(),
				runv1.RunStatus_RUN_STATUS_FAILED.String(),
				runv1.RunStatus_RUN_STATUS_CANCELLED.String(),
			},
		},
		{
			name: "live environments",
			query: `SELECT EXISTS (
				SELECT 1 FROM environments
				WHERE namespace = $1
			)`,
		},
		{
			name: "secrets",
			query: `SELECT EXISTS (
				SELECT 1 FROM secrets
				WHERE namespace = $1
			)`,
		},
	}
	for _, check := range checks {
		var exists bool
		args := append([]any{namespace}, check.args...)
		if err := q.QueryRow(ctx, check.query, args...).Scan(&exists); err != nil {
			return fmt.Errorf("check namespace %s: %w", check.name, err)
		}
		if exists {
			return grpcstatus.Errorf(codes.FailedPrecondition, "namespace %q has %s", namespace, check.name)
		}
	}
	return nil
}

type namespaceScanner interface {
	Scan(dest ...any) error
}

func scanNamespace(row namespaceScanner) (*namespacev1.Namespace, error) {
	var (
		namespace string
		createdAt time.Time
		deletedAt *time.Time
	)
	if err := row.Scan(&namespace, &createdAt, &deletedAt); err != nil {
		return nil, err
	}
	record := &namespacev1.Namespace{
		Namespace: namespace,
		CreatedAt: timestamppb.New(createdAt),
	}
	if deletedAt != nil {
		record.DeletedAt = timestamppb.New(*deletedAt)
	}
	return record, nil
}
