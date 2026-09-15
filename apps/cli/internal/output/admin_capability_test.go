package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderAllocationCapabilityDiagnostics(t *testing.T) {
	now := time.Now().UTC()
	key := capabilitycontract.ExtensionKey("example.com/accelerator", "model-a")
	requirement := &capabilityv1.CapabilityRequirement{Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY}
	diagnostics := &adminv1.GetAllocationCapabilityDiagnosticsResponse{
		Requirements: []*capabilityv1.CapabilityRequirement{requirement},
		ConditionSet: &capabilityv1.CapabilityConditionSet{
			ObservedAt: timestamppb.New(now),
			Conditions: []*capabilityv1.CapabilityCondition{{
				Key:        key,
				State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			}},
		},
	}
	var buffer bytes.Buffer
	RenderAllocationCapabilityDiagnostics(&buffer, diagnostics)
	output := buffer.String()
	for _, expected := range []string{"CAPABILITY", "LOSS POLICY", "example.com/accelerator=model-a", "admission_only"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output)
		}
	}
}
