package terminal

import "time"

type Options struct {
	IdleTimeout        time.Duration
	MaxDuration        time.Duration
	LeaseRetryAttempts int
	LeaseRetryDelay    time.Duration
}
