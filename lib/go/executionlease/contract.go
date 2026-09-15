// Package executionlease defines the narrow timing contract shared by
// controld and axnoded. ExecutionLease is finite Allocation liveness authority,
// not a persisted control-plane entity or a data-plane credential.
package executionlease

import "time"

const (
	TTL                = 30 * time.Second
	MaxRenewalInterval = TTL / 3
)
