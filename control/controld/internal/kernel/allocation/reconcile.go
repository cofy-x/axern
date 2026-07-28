package allocationkernel

import "time"

const (
	ReconcileReasonCreate   = "create"
	ReconcileReasonDelete   = "delete"
	DefaultReconcileLimit   = 20
	DeleteRetryDelay        = 5 * time.Second
	CreateRetryInitialDelay = 2 * time.Second
	CreateRetryMaxDelay     = 30 * time.Second
	CreateRetryMaxAttempts  = 5
)

type ReconcileItem struct {
	AllocationID       string
	OwnerID            string
	EnvironmentID      string
	Reason             string
	NodeID             string
	NodeTarget         string
	Attempt            int64
	ReconcileAttempts  int
	LastReconcileError string
	ClaimOwner         string
	NextRunAt          time.Time
	EligibleAt         time.Time
}

type ScheduleReconcileRequest struct {
	AllocationID       string
	Reason             string
	NextRunAt          time.Time
	LastReconcileError string
	IncrementAttempts  bool
}

type CreateRetryPlan struct {
	FailureNumber int
	Retry         bool
	NextRunAt     time.Time
}

type LifecycleRetryFilter struct {
	OwnerType string
	Reason    string
	DueOnly   bool
	Limit     int
}

type ForceLifecycleRetryRequest struct {
	AllocationID   string
	Reason         string
	OperatorReason string
	RequestedRunAt time.Time
}

type FailLifecycleRetryRequest struct {
	AllocationID   string
	Reason         string
	OperatorReason string
}

type ClearLifecycleRetryRequest struct {
	AllocationID   string
	Reason         string
	OperatorReason string
}

type LifecycleRetryItem struct {
	AllocationID       string    `json:"allocation_id"`
	OwnerID            string    `json:"owner_id"`
	OwnerType          string    `json:"owner_type"`
	EnvironmentID      string    `json:"environment_id,omitempty"`
	Reason             string    `json:"reason"`
	NodeID             string    `json:"node_id"`
	NodeTarget         string    `json:"node_target,omitempty"`
	Attempt            int64     `json:"attempt"`
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
		Reason:             ReconcileReasonCreate,
		NextRunAt:          plan.NextRunAt,
		LastReconcileError: lastError,
		IncrementAttempts:  true,
	}, true
}

func ScheduleDeleteRetryRequest(allocationID string, lastError string, now time.Time) ScheduleReconcileRequest {
	return ScheduleReconcileRequest{
		AllocationID:       allocationID,
		Reason:             ReconcileReasonDelete,
		NextRunAt:          now.Add(DeleteRetryDelay),
		LastReconcileError: lastError,
		IncrementAttempts:  true,
	}
}

func ScheduleDeleteRequest(allocationID string, now time.Time) ScheduleReconcileRequest {
	return ScheduleReconcileRequest{
		AllocationID: allocationID,
		Reason:       ReconcileReasonDelete,
		NextRunAt:    now,
	}
}

func ScheduleImmediateDeleteRetryRequest(allocationID string, lastError string, now time.Time) ScheduleReconcileRequest {
	request := ScheduleDeleteRequest(allocationID, now)
	request.LastReconcileError = lastError
	return request
}
