package pgrun

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestRunAllocationObservationMatches(t *testing.T) {
	allocation := &runStatusAllocation{
		status:         commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		exitCode:       0,
		exitCodeKnown:  false,
		diagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		message:        "running",
	}
	observation := &nodev1.AllocationStatusObservation{
		Status:  commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		Message: " running ",
	}
	if !runAllocationObservationMatches(allocation, observation, observation.GetDiagnosticCode(), "running") {
		t.Fatal("identical run observation was not recognized")
	}
	if runAllocationObservationMatches(allocation, observation, observation.GetDiagnosticCode(), "updated") {
		t.Fatal("changed run observation was treated as identical")
	}
	if runAllocationObservationMatches(allocation, observation, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, "running") {
		t.Fatal("changed diagnostic code was treated as identical")
	}
}
