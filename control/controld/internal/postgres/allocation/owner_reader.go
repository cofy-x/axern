package pgallocation

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ownerQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type OwnerReader struct {
	queryer ownerQueryer
}

func NewOwnerReader(queryer ownerQueryer) *OwnerReader {
	return &OwnerReader{queryer: queryer}
}

func (r *OwnerReader) ResolveAllocationOwners(ctx context.Context, allocationIDs []string) (map[string]string, error) {
	allocationIDs = compactAllocationIDs(allocationIDs)
	owners := make(map[string]string, len(allocationIDs))
	if r == nil || r.queryer == nil || len(allocationIDs) == 0 {
		return owners, nil
	}
	rows, err := r.queryer.Query(ctx, `
		SELECT allocation_id, owner_type
		FROM allocations
		WHERE allocation_id = ANY($1::text[])
	`, allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve allocation owners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var allocationID, ownerType string
		if err := rows.Scan(&allocationID, &ownerType); err != nil {
			return nil, fmt.Errorf("scan allocation owner: %w", err)
		}
		owners[strings.TrimSpace(allocationID)] = strings.TrimSpace(ownerType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate allocation owners: %w", err)
	}
	return owners, nil
}
