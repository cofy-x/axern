package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsDependencyUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "connection exception", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "admin shutdown", err: &pgconn.PgError{Code: "57P01"}, want: true},
		{name: "crash shutdown", err: &pgconn.PgError{Code: "57P02"}, want: true},
		{name: "cannot connect now", err: fmt.Errorf("query: %w", &pgconn.PgError{Code: "57P03"}), want: true},
		{name: "constraint violation", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "non postgres", err: errors.New("unavailable"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDependencyUnavailable(tt.err); got != tt.want {
				t.Fatalf("IsDependencyUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}
