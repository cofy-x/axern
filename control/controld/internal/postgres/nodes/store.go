package pgnodes

import (
	"context"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

type PGStore struct {
	db *postgres.DB
}

func NewPGStore(db *postgres.DB) *PGStore {
	return &PGStore{db: db}
}

func (s *PGStore) Register(ctx context.Context, params nodekernel.RegisterParams) (*nodekernel.Record, error) {
	return s.upsert(ctx, nodeUpsertParams{
		NodeID:        params.NodeID,
		NodeTarget:    params.NodeTarget,
		Runtimes:      params.Runtimes,
		NodeAuthToken: params.NodeAuthToken,
		Now:           params.Now,
	})
}

func (s *PGStore) Report(ctx context.Context, params nodekernel.ReportParams) (*nodekernel.Record, error) {
	return s.upsert(ctx, nodeUpsertParams{
		NodeID:        params.NodeID,
		NodeTarget:    params.NodeTarget,
		Runtimes:      params.Runtimes,
		Summary:       params.Summary,
		NodeAuthToken: params.NodeAuthToken,
		Now:           params.Now,
	})
}
