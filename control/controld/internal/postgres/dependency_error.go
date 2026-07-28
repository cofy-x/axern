package postgres

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

func IsDependencyUnavailable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return strings.HasPrefix(pgErr.Code, "08") ||
		pgErr.Code == "57P01" ||
		pgErr.Code == "57P02" ||
		pgErr.Code == "57P03"
}
