package placement

import (
	"context"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

const (
	SelectionModeCandidates = "candidates"

	SelectionResultOK         = "ok"
	SelectionResultError      = "error"
	SelectionResultNoEligible = "no_eligible"
	SelectionResultRetryable  = "retryable"
)

type Observer interface {
	RecordSelection(context.Context, SelectionObservation)
}

type SelectionObservation struct {
	Mode                           string
	Result                         string
	MountType                      nodev1.MountType
	RequestedCPUMilli              int64
	RequestedMemoryBytes           int64
	RequestedEphemeralStorageBytes int64
	EligibleCount                  int
	RejectedCount                  int
	RejectionReasons               []placementkernel.RejectionReason
}
