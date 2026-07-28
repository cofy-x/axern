package pgservice

import (
	"context"
	"fmt"
	"sort"
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) Get(ctx context.Context, id string) (*servicev1.Service, bool, error) {
	service, err := scanService(s.db.Pool().QueryRow(ctx, serviceSelectSQL()+` WHERE service_id = $1`, strings.TrimSpace(id)))
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := s.enrichService(ctx, service); err != nil {
		return nil, false, err
	}
	return service, true, nil
}

func (s *PGStore) List(ctx context.Context, filter *servicev1.ServiceListFilter) ([]*servicev1.Service, error) {
	rows, err := s.db.Pool().Query(ctx, serviceSelectSQL()+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()
	out := make([]*servicev1.Service, 0)
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		if servicekernel.MatchFilter(service, filter) {
			out = append(out, service)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.enrichServices(ctx, out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GetCreatedAt().AsTime().After(out[j].GetCreatedAt().AsTime())
	})
	return out, nil
}

func (s *PGStore) ListAutoscaled(ctx context.Context) ([]*servicev1.Service, error) {
	rows, err := s.db.Pool().Query(ctx, serviceSelectSQL()+` WHERE autoscaling_policy <> 'null'::jsonb ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query autoscaled services: %w", err)
	}
	defer rows.Close()
	out := make([]*servicev1.Service, 0)
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, service)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
