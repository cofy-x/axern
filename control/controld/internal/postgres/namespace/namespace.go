package namespace

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func Ensure(ctx context.Context, q execer, namespace string) (string, error) {
	return ensureAt(ctx, q, namespace, time.Time{})
}

func normalizeNamespace(namespace string) string {
	return environmentkernel.NormalizeNamespace(namespace)
}

func LockQuotaPolicy(ctx context.Context, tx pgx.Tx, namespace string) (resourcekernel.NamespaceQuotaPolicy, error) {
	normalized, err := LockQuotaRows(ctx, tx, namespace)
	if err != nil {
		return resourcekernel.NamespaceQuotaPolicy{}, err
	}
	var cpuLimit sql.NullInt64
	var memoryLimit sql.NullInt64
	if err := tx.QueryRow(ctx, `
		SELECT cpu_milli_limit, memory_bytes_limit
		FROM namespace_resource_quotas
		WHERE namespace = $1
	`, normalized).Scan(&cpuLimit, &memoryLimit); err != nil {
		return resourcekernel.NamespaceQuotaPolicy{}, fmt.Errorf("load namespace quota policy: %w", err)
	}
	return resourcekernel.NamespaceQuotaPolicy{
		CPUMilliLimit:    nullableInt64(cpuLimit),
		MemoryBytesLimit: nullableInt64(memoryLimit),
	}, nil
}

func LockQuotaRows(ctx context.Context, tx pgx.Tx, namespace string) (string, error) {
	normalized, err := Ensure(ctx, tx, namespace)
	if err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx, `
		SELECT n.namespace
		FROM namespaces n
		JOIN namespace_resource_quotas q ON q.namespace = n.namespace
		WHERE n.namespace = $1
		FOR UPDATE OF n, q
	`, normalized).Scan(&normalized); err != nil {
		return "", fmt.Errorf("lock namespace quota: %w", err)
	}
	return normalized, nil
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}
