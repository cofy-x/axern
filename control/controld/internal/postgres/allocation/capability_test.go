package pgallocation

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

type capabilityRecordingExecutor struct {
	calls [][]any
}

func (e *capabilityRecordingExecutor) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	e.calls = append(e.calls, args)
	return capabilityRecordingRow{}
}

type capabilityRecordingRow struct{}

func (capabilityRecordingRow) Scan(dest ...any) error {
	*(dest[0].(*int)) = 1
	return nil
}

func TestRecordCapabilityVerificationPersistsAuthoritativeDependencyEvidence(t *testing.T) {
	executor := &capabilityRecordingExecutor{}
	dependency := &capabilityv1.CapabilityDependency{
		Key: &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{
			Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
		}},
		LossPolicy:       capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP,
		SelectedEvidence: &capabilityv1.CapabilityEvidence{EvidenceID: "create-evidence"},
		DependencyEvidence: []*capabilityv1.CapabilityEvidenceReference{{
			Key:        &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER}},
			EvidenceID: "cgroup-evidence",
		}},
	}
	admission := &allocationkernel.CapabilityAdmission{
		Dependencies: []*capabilityv1.CapabilityDependency{dependency},
		Conditions: []*capabilityv1.CapabilityCondition{{
			Key: dependency.GetKey(), State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			Evidence: dependency.GetSelectedEvidence(),
		}},
	}
	if err := RecordCapabilityVerification(context.Background(), executor, "alloc-1", admission, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("query calls = %d, want one atomic allocation/run update", len(executor.calls))
	}
	encoded, ok := executor.calls[0][2].(string)
	if !ok {
		t.Fatalf("admitted dependency payload type = %T", executor.calls[0][2])
	}
	stored := &capabilityv1.CapabilityDependencySet{}
	if err := protojson.Unmarshal([]byte(encoded), stored); err != nil {
		t.Fatal(err)
	}
	got := stored.GetDependencies()
	if len(got) != 1 || got[0].GetSelectedEvidence().GetEvidenceID() != "create-evidence" || len(got[0].GetDependencyEvidence()) != 1 || got[0].GetDependencyEvidence()[0].GetEvidenceID() != "cgroup-evidence" {
		t.Fatalf("stored admitted dependencies = %#v", got)
	}
}
