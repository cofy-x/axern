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

func TestRenderAllocationCapabilityDiagnosticsIncludesProofAndAttemptIdentity(t *testing.T) {
	now := time.Now().UTC()
	key := capabilitycontract.ExtensionKey("example.com/accelerator", "model-a")
	proof := &capabilityv1.CapabilityObservationProof{
		Key:           key,
		ObservationID: "observation-1",
		Provider:      capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		ObservedAt:    timestamppb.New(now),
		Evidence:      capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	dependency := &capabilityv1.CapabilityDependency{
		Key:                 key,
		LossPolicy:          capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY,
		SelectedSnapshot:    &capabilityv1.CapabilitySnapshotReference{SnapshotID: "snapshot-1"},
		SelectedObservation: proof,
	}
	diagnostics := &adminv1.GetAllocationCapabilityDiagnosticsResponse{
		AllocationAttempt:         7,
		CreateAdmissionRecorded:   true,
		CreateDependencySetDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreateAdmittedAt:          timestamppb.New(now),
		RequiredDependencies:      []*capabilityv1.CapabilityDependency{dependency},
		ConditionSet: &capabilityv1.CapabilityConditionSet{
			Revision:   3,
			ObservedAt: timestamppb.New(now),
			Conditions: []*capabilityv1.CapabilityCondition{{
				Key:        key,
				State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
				ObservedAt: timestamppb.New(now),
				Proof:      proof,
			}},
		},
	}
	var buffer bytes.Buffer
	RenderAllocationCapabilityDiagnostics(&buffer, diagnostics)
	output := buffer.String()
	for _, expected := range []string{"PROVIDER", "SNAPSHOT", "DEPENDENCIES", "config", "snapshot-1", "ATTEMPT", "7", "CREATE ADMISSION", "true", "REVISION", "3"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output)
		}
	}
}
