package pgrun

import (
	"testing"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestRunAllocationObservationMatches(t *testing.T) {
	allocation := &runStatusAllocation{
		status:               commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		exitCode:             0,
		exitCodeKnown:        false,
		message:              "running",
		capabilityConditions: &capabilityv1.CapabilityConditionSet{},
	}
	observation := &nodev1.AllocationStatusObservation{
		Status:  commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		Message: " running ",
	}
	if !runAllocationObservationMatches(allocation, observation, "running", &capabilityv1.CapabilityConditionSet{}) {
		t.Fatal("identical run observation was not recognized")
	}
	if runAllocationObservationMatches(allocation, observation, "updated", &capabilityv1.CapabilityConditionSet{}) {
		t.Fatal("changed run observation was treated as identical")
	}
}
