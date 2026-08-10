package runtime

import (
	"context"
	"errors"
	"testing"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func TestMemoryCapabilityVerifierRejectsMissingPositiveLimit(t *testing.T) {
	options := contract.HandlerOptions{ContainerID: "allocation-without-limit"}
	runcDependency := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)}
	if result := (&RuncServiceHandler{}).VerifyAllocationCapability(context.Background(), runcDependency, options); result.State != contract.CapabilityVerificationLost {
		t.Fatal("runc memory verifier accepted a missing hard limit")
	}
	runscDependency := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT)}
	if result := (&RunscServiceHandler{}).VerifyAllocationCapability(context.Background(), runscDependency, options); result.State != contract.CapabilityVerificationLost {
		t.Fatal("runsc memory verifier accepted a missing hard limit")
	}
}

func TestCapabilityVerifierClassifiesStateReadErrorsAsInconclusive(t *testing.T) {
	result := classifyCapabilityVerificationError(inconclusiveCapabilityErrorf("read runtime state: %w", errors.New("temporary runtime control failure")))
	if result.State != contract.CapabilityVerificationInconclusive {
		t.Fatalf("classification = %v, want inconclusive", result.State)
	}
}
