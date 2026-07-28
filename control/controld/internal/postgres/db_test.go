package postgres

import "testing"

func TestWithMaxConnectionsOverridesParsedPoolConfig(t *testing.T) {
	cfg, err := poolConfig("postgres://postgres:postgres@localhost/axern?sslmode=disable")
	if err != nil {
		t.Fatalf("poolConfig() error = %v", err)
	}
	WithMaxConnections(48)(cfg)
	if cfg.MaxConns != 48 {
		t.Fatalf("MaxConns = %d, want 48", cfg.MaxConns)
	}
}

func TestWithMaxConnectionsIgnoresNonPositiveValues(t *testing.T) {
	cfg, err := poolConfig("postgres://postgres:postgres@localhost/axern?sslmode=disable")
	if err != nil {
		t.Fatalf("poolConfig() error = %v", err)
	}
	want := cfg.MaxConns
	WithMaxConnections(0)(cfg)
	if cfg.MaxConns != want {
		t.Fatalf("MaxConns = %d, want default %d", cfg.MaxConns, want)
	}
}
