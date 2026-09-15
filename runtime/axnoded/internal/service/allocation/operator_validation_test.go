package allocation

import (
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

const operatorValidationDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestValidateOperatorExecutionRequiresExactLiveAllocationState(t *testing.T) {
	fixture := newTestAllocationController(t, nil)
	now := time.Now().UTC()
	fixture.controller.allocationStates["allocation-1"] = &allocationState{
		record: &apipb.AllocationState{
			AllocationID:                    "allocation-1",
			NodeID:                          "node-1",
			AllocationRequestDigest:         operatorValidationDigest,
			ExecutionLeaseExpiresAtUnixNano: now.Add(time.Minute).UnixNano(),
			EnforcementManifest:             &apipb.AllocationEnforcementManifest{},
		},
		runtime: &environmentcache.PreparedEnvironment{},
	}
	if err := fixture.controller.ValidateOperatorExecution("allocation-1", now); err != nil {
		t.Fatalf("ValidateOperatorExecution() error = %v", err)
	}
	fixture.controller.allocationStates["allocation-1"].record.ExecutionLeaseExpiresAtUnixNano = now.Add(-time.Second).UnixNano()
	if err := fixture.controller.ValidateOperatorExecution("allocation-1", now); err == nil {
		t.Fatal("ValidateOperatorExecution() accepted an expired control-plane lease")
	}
}

func TestValidateOperatorRecoveryAllowsRetryAfterTerminationIntent(t *testing.T) {
	fixture := newTestAllocationController(t, nil)
	fixture.controller.allocationStates["allocation-1"] = &allocationState{
		record: &apipb.AllocationState{
			AllocationID:              "allocation-1",
			AllocationRequestDigest:   operatorValidationDigest,
			TerminationDiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_OPERATOR_FORCE_CLEANUP,
		},
		runtime: &environmentcache.PreparedEnvironment{},
	}
	if err := fixture.controller.ValidateOperatorRecovery("allocation-1"); err != nil {
		t.Fatalf("ValidateOperatorRecovery() error = %v", err)
	}
	if err := fixture.controller.ValidateOperatorExecution("allocation-1", time.Now().UTC()); err == nil {
		t.Fatal("ValidateOperatorExecution() accepted a terminating Allocation")
	}
}
