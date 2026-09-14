package pgrun

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestRunAllocationObservationMatches(t *testing.T) {
	allocation := &reportedAllocation{
		lifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		runStatus:      runv1.RunStatus_RUN_STATUS_RUNNING,
		diagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		message:        "running",
	}
	observation := &nodev1.AllocationLifecycleObservation{
		State:   commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		Message: " running ",
	}
	if !runAllocationObservationMatches(allocation, observation.GetState(), runv1.RunStatus_RUN_STATUS_RUNNING, observation, observation.GetDiagnosticCode(), "running") {
		t.Fatal("identical run observation was not recognized")
	}
	if runAllocationObservationMatches(allocation, observation.GetState(), runv1.RunStatus_RUN_STATUS_RUNNING, observation, observation.GetDiagnosticCode(), "updated") {
		t.Fatal("changed run observation was treated as identical")
	}
	if runAllocationObservationMatches(allocation, observation.GetState(), runv1.RunStatus_RUN_STATUS_RUNNING, observation, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, "running") {
		t.Fatal("changed diagnostic code was treated as identical")
	}
}
