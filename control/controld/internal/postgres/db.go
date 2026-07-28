package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const connectTimeout = 5 * time.Second

type DB struct {
	pool *pgxpool.Pool
}

type PoolOption func(*pgxpool.Config)

func WithMaxConnections(maxConnections int32) PoolOption {
	return func(cfg *pgxpool.Config) {
		if maxConnections > 0 {
			cfg.MaxConns = maxConnections
		}
	}
}

func Open(ctx context.Context, dsn string, options ...PoolOption) (*DB, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	cfg, err := poolConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	for _, option := range options {
		if option != nil {
			option(cfg)
		}
	}

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &DB{pool: pool}, nil
}

func poolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	return cfg, nil
}

func (db *DB) Close() {
	if db == nil || db.pool == nil {
		return
	}
	db.pool.Close()
}

func (db *DB) Pool() *pgxpool.Pool {
	if db == nil {
		return nil
	}
	return db.pool
}
