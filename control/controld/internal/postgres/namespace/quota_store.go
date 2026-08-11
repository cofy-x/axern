package namespace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (s *Store) Get(ctx context.Context, namespace string) (*quotav1.NamespaceQuota, error) {
	normalized := normalizeNamespace(namespace)
	return queryQuota(ctx, s.db.Pool(), normalized)
}

func (s *Store) List(ctx context.Context) ([]*quotav1.NamespaceQuota, error) {
	rows, err := s.db.Pool().Query(ctx, quotaSelectSQL("")+`
		ORDER BY q.namespace
	`)
	if err != nil {
		return nil, fmt.Errorf("list namespace quotas: %w", err)
	}
	defer rows.Close()
	var quotas []*quotav1.NamespaceQuota
	for rows.Next() {
		quota, err := scanQuota(rows)
		if err != nil {
			return nil, err
		}
		quotas = append(quotas, quota)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list namespace quotas: %w", err)
	}
	return quotas, nil
}

func (s *Store) Set(ctx context.Context, namespace string, limits *quotav1.NamespaceQuotaLimits, now time.Time) (*quotav1.NamespaceQuota, error) {
	if limits == nil {
		limits = &quotav1.NamespaceQuotaLimits{}
	}
	return s.withTx(ctx, func(tx pgx.Tx) (*quotav1.NamespaceQuota, error) {
		normalized, err := LockQuotaRows(ctx, tx, namespace)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE namespace_resource_quotas
			SET cpu_milli_limit = $2,
			    memory_bytes_limit = $3,
			    ephemeral_storage_bytes_limit = $4,
			    version = version + 1,
			    updated_at = $5
			WHERE namespace = $1
		`, normalized, nullableLimit(limits.GetCpuMilli()), nullableLimit(limits.GetMemoryBytes()), nullableLimit(limits.GetEphemeralStorageBytes()), now); err != nil {
			return nil, fmt.Errorf("set namespace quota: %w", err)
		}
		return queryQuota(ctx, tx, normalized)
	})
}

func (s *Store) Unset(ctx context.Context, namespace string, now time.Time) (*quotav1.NamespaceQuota, error) {
	return s.withTx(ctx, func(tx pgx.Tx) (*quotav1.NamespaceQuota, error) {
		normalized, err := LockQuotaRows(ctx, tx, namespace)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE namespace_resource_quotas
			SET cpu_milli_limit = NULL,
			    memory_bytes_limit = NULL,
			    ephemeral_storage_bytes_limit = NULL,
			    version = version + 1,
			    updated_at = $2
			WHERE namespace = $1
	`, normalized, now); err != nil {
			return nil, fmt.Errorf("unset namespace quota: %w", err)
		}
		return queryQuota(ctx, tx, normalized)
	})
}

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) (*quotav1.NamespaceQuota, error)) (*quotav1.NamespaceQuota, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	quota, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return quota, nil
}

func queryQuota(ctx context.Context, q queryer, namespace string) (*quotav1.NamespaceQuota, error) {
	row := q.QueryRow(ctx, quotaSelectSQL("WHERE q.namespace = $1"), namespace)
	quota, err := scanQuota(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "namespace quota %q not found", namespace)
	}
	if err != nil {
		return nil, fmt.Errorf("get namespace quota: %w", err)
	}
	return quota, nil
}

type quotaScanner interface {
	Scan(dest ...any) error
}

func scanQuota(row quotaScanner) (*quotav1.NamespaceQuota, error) {
	var (
		namespace                string
		cpuLimit                 sql.NullInt64
		memoryLimit              sql.NullInt64
		ephemeralStorageLimit    sql.NullInt64
		reservedCPU              int64
		reservedMemory           int64
		reservedEphemeralStorage int64
		version                  int64
		createdAt, updatedAt     time.Time
	)
	if err := row.Scan(&namespace, &cpuLimit, &memoryLimit, &ephemeralStorageLimit, &reservedCPU, &reservedMemory, &reservedEphemeralStorage, &version, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return &quotav1.NamespaceQuota{
		Namespace:                      namespace,
		CpuMilliLimit:                  optionalInt64(cpuLimit),
		MemoryBytesLimit:               optionalInt64(memoryLimit),
		ReservedCpuMilli:               reservedCPU,
		ReservedMemoryBytes:            reservedMemory,
		AvailableCpuMilli:              optionalAvailable(cpuLimit, reservedCPU),
		AvailableMemoryBytes:           optionalAvailable(memoryLimit, reservedMemory),
		EphemeralStorageBytesLimit:     optionalInt64(ephemeralStorageLimit),
		ReservedEphemeralStorageBytes:  reservedEphemeralStorage,
		AvailableEphemeralStorageBytes: optionalAvailable(ephemeralStorageLimit, reservedEphemeralStorage),
		Version:                        version,
		CreatedAt:                      timestamppb.New(createdAt),
		UpdatedAt:                      timestamppb.New(updatedAt),
	}, nil
}

func quotaSelectSQL(where string) string {
	return `
		SELECT q.namespace,
		       q.cpu_milli_limit,
		       q.memory_bytes_limit,
		       q.ephemeral_storage_bytes_limit,
		       COALESCE(SUM(w.cpu_milli), 0) AS reserved_cpu_milli,
		       COALESCE(SUM(w.sandbox_memory_request_bytes), 0) AS reserved_memory_bytes,
		       COALESCE(SUM(w.ephemeral_storage_bytes), 0) AS reserved_ephemeral_storage_bytes,
		       q.version,
		       q.created_at,
		       q.updated_at
		FROM namespace_resource_quotas q
		LEFT JOIN workload_reservations w ON w.namespace = q.namespace AND w.released_at IS NULL
		` + where + `
		GROUP BY q.namespace, q.cpu_milli_limit, q.memory_bytes_limit, q.ephemeral_storage_bytes_limit, q.version, q.created_at, q.updated_at
	`
}

func optionalInt64(value sql.NullInt64) *wrapperspb.Int64Value {
	if !value.Valid {
		return nil
	}
	return wrapperspb.Int64(value.Int64)
}

func optionalAvailable(limit sql.NullInt64, used int64) *wrapperspb.Int64Value {
	if !limit.Valid {
		return nil
	}
	available := limit.Int64 - used
	if available < 0 {
		available = 0
	}
	return wrapperspb.Int64(available)
}

func nullableLimit(value *wrapperspb.Int64Value) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value.Value, Valid: true}
}
