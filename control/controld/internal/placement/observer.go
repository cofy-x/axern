package placement

import (
	"context"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
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
	Mode                        string
	Result                      string
	Runtime                     string
	MountType                   nodev1.MountType
	RequestedCPUMilli           int64
	RequestedMemoryBytes        int64
	RequestedWritableLayerBytes int64
	EligibleCount               int
	RejectedCount               int
	RejectionReasons            []nodev1.PlacementRejectionReason
}
