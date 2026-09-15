package terminal

import "time"

type Options struct {
	IdleTimeout              time.Duration
	MaxDuration              time.Duration
	AccessGrantRetryAttempts int
	AccessGrantRetryDelay    time.Duration
}
