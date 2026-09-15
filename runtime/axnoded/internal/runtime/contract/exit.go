package contract

import (
	"errors"
	"time"
)

var ErrExitStatusUnavailable = errors.New("exit status unavailable")

func IsExitStatusUnavailable(err error) bool {
	return errors.Is(err, ErrExitStatusUnavailable)
}

type Exit struct {
	Timestamp time.Time
	Status    int
}
