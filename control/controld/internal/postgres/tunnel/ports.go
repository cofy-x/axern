package pgtunnel

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func allocateRemotePort(ctx context.Context, tx portQuerier, allocationID string) (int32, error) {
	rows, err := tx.Query(ctx, `
		SELECT remote_port
		FROM tunnel_sessions
		WHERE allocation_id = $1
		  AND revoked = FALSE
		  AND status IN (
			'TUNNEL_SESSION_STATUS_PENDING',
			'TUNNEL_SESSION_STATUS_RUNNING',
			'TUNNEL_SESSION_STATUS_DEGRADED'
		  )
	`, allocationID)
	if err != nil {
		return 0, fmt.Errorf("query active tunnel ports: %w", err)
	}
	defer rows.Close()
	used := map[int32]struct{}{}
	for rows.Next() {
		var port int32
		if err := rows.Scan(&port); err != nil {
			return 0, fmt.Errorf("scan active tunnel port: %w", err)
		}
		used[port] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active tunnel ports: %w", err)
	}
	span := int32(autoPortMax - autoPortMin + 1)
	for range 128 {
		port := int32(autoPortMin) + int32(randomUint32()%uint32(span))
		if _, ok := used[port]; !ok {
			return port, nil
		}
	}
	for port := int32(autoPortMin); port <= int32(autoPortMax); port++ {
		if _, ok := used[port]; !ok {
			return port, nil
		}
	}
	return 0, grpcstatus.Error(codes.ResourceExhausted, "no automatic tunnel remote ports are available for this allocation")
}
