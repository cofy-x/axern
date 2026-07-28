package namespace

import (
	"context"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/jackc/pgx/v5"
)

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	db *postgres.DB
}

func NewStore(db *postgres.DB) *Store {
	return &Store{db: db}
}
