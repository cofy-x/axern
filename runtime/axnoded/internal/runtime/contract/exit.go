package contract

import (
	"errors"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

var ErrExitStatusUnavailable = errors.New("exit status unavailable")

func IsExitStatusUnavailable(err error) bool {
	return errors.Is(err, ErrExitStatusUnavailable)
}

type Exit struct {
	Timestamp          time.Time
	Status             int
	ManagedProxyReport *apipb.ManagedProxyReport
}
