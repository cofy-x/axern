package postgres

import "errors"

type CommitError interface {
	CommitTransaction() bool
}

func ShouldCommitError(err error) bool {
	var commitErr CommitError
	return errors.As(err, &commitErr) && commitErr.CommitTransaction()
}
