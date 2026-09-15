package allocationkernel

import (
	"errors"
	"time"

	"github.com/cofy-x/axern/lib/go/executionlease"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

var ErrReconcileClaimLost = errors.New("allocation reconcile claim lost")

const (
	DefaultReconcileLimit     = 20
	DeleteRetryDelay          = 5 * time.Second
	CreateRetryInitialDelay   = 2 * time.Second
	CreateRetryMaxDelay       = 30 * time.Second
	CreateRetryMaxAttempts    = 5
	CreateExecutionTimeout    = 10 * time.Minute
	LifecycleOperationTimeout = 60 * time.Second
	ReconcileClaimTTL         = 30 * time.Second
	ReconcileClaimRenewal     = 10 * time.Second
	ReconcileWorkerCount      = 8
	ExecutionLeaseTTL         = executionlease.TTL
)

type ReconcileItem struct {
	OutputExpiresAt        *time.Time
	AllocationID           string
	RunID                  string
	EnvironmentID          string
	LifecycleState         commonv1.AllocationLifecycleState
	NodeID                 string
	NodeTarget             string
	ReconcileAttempts      int
	LastReconcileError     string
	ClaimOwner             string
	NextRunAt              time.Time
	EligibleAt             time.Time
	CapabilityRequirements []*capabilityv1.CapabilityRequirement
}

type ScheduleReconcileRequest struct {
	AllocationID       string
	Intent             ReconcileIntent
	NextRunAt          time.Time
	LastReconcileError string
	IncrementAttempts  bool
}

type ReconcileIntent uint8

const (
	ReconcileIntentUnspecified ReconcileIntent = iota
	ReconcileIntentEnsurePresent
	ReconcileIntentEnsureAbsent
)

func ReconcileIntentForLifecycle(state commonv1.AllocationLifecycleState) ReconcileIntent {
	switch state {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE:
		return ReconcileIntentEnsurePresent
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED:
		return ReconcileIntentEnsureAbsent
	default:
		return ReconcileIntentUnspecified
	}
}

type CreateRetryPlan struct {
	FailureNumber int
	Retry         bool
	NextRunAt     time.Time
}

type LifecycleRetryFilter struct {
	DueOnly bool
	Limit   int
}

type ForceLifecycleRetryRequest struct {
	AllocationID   string
	OperatorReason string
	RequestedRunAt time.Time
}

type FailLifecycleRetryRequest struct {
	AllocationID   string
	OperatorReason string
}

type ClearLifecycleRetryRequest struct {
	AllocationID   string
	OperatorReason string
}

type LifecycleRetryItem struct {
	AllocationID       string    `json:"allocation_id"`
	RunID              string    `json:"run_id"`
	EnvironmentID      string    `json:"environment_id,omitempty"`
	LifecycleState     string    `json:"lifecycle_state"`
	NodeID             string    `json:"node_id"`
	NodeTarget         string    `json:"node_target,omitempty"`
	ReconcileAttempts  int       `json:"reconcile_attempts"`
	LastReconcileError string    `json:"last_error,omitempty"`
	NextRunAt          time.Time `json:"next_run_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	AgeSeconds         int64     `json:"age_seconds"`
	Due                bool      `json:"due"`
	Clearable          bool      `json:"clearable"`
	ClearBlockedReason string    `json:"clear_blocked_reason,omitempty"`
}

func CreateRetryDelay(failureNumber int) time.Duration {
	if failureNumber <= 0 {
		failureNumber = 1
	}
	delay := CreateRetryInitialDelay
	for i := 1; i < failureNumber; i++ {
		delay *= 2
		if delay >= CreateRetryMaxDelay {
			return CreateRetryMaxDelay
		}
	}
	return delay
}

func PlanCreateRetry(currentAttempts int, now time.Time) CreateRetryPlan {
	failureNumber := currentAttempts + 1
	if failureNumber >= CreateRetryMaxAttempts {
		return CreateRetryPlan{FailureNumber: failureNumber}
	}
	return CreateRetryPlan{
		FailureNumber: failureNumber,
		Retry:         true,
		NextRunAt:     now.Add(CreateRetryDelay(failureNumber)),
	}
}

func ScheduleCreateRetryRequest(allocationID string, currentAttempts int, lastError string, now time.Time) (ScheduleReconcileRequest, bool) {
	plan := PlanCreateRetry(currentAttempts, now)
	if !plan.Retry {
		return ScheduleReconcileRequest{}, false
	}
	return ScheduleReconcileRequest{
		AllocationID:       allocationID,
		Intent:             ReconcileIntentEnsurePresent,
		NextRunAt:          plan.NextRunAt,
		LastReconcileError: lastError,
		IncrementAttempts:  true,
	}, true
}

func ScheduleDeleteRetryRequest(allocationID string, lastError string, now time.Time) ScheduleReconcileRequest {
	return ScheduleReconcileRequest{
		AllocationID:       allocationID,
		Intent:             ReconcileIntentEnsureAbsent,
		NextRunAt:          now.Add(DeleteRetryDelay),
		LastReconcileError: lastError,
		IncrementAttempts:  true,
	}
}

func ScheduleDeleteRequest(allocationID string, now time.Time) ScheduleReconcileRequest {
	return ScheduleReconcileRequest{
		AllocationID: allocationID,
		Intent:       ReconcileIntentEnsureAbsent,
		NextRunAt:    now,
	}
}
