package allocationkernel

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestReconcileItemOutputSealingPresenceDistinguishesOrdinaryDeleteAndZeroOutputs(t *testing.T) {
	if got := (ReconcileItem{}).OutputSealingRequest(); got != nil {
		t.Fatalf("ordinary delete output sealing = %#v, want nil", got)
	}
	expiresAt := time.Now().Add(time.Hour).UTC()
	got := (ReconcileItem{OutputExpiresAt: &expiresAt}).OutputSealingRequest()
	if got == nil || !got.ExpiresAt.Equal(expiresAt) || len(got.Outputs) != 0 {
		t.Fatalf("explicit zero-output sealing = %#v", got)
	}
	item := ReconcileItem{OutputExpiresAt: &expiresAt, DeclaredOutputs: []*commonv1.DeclaredOutput{{Path: "/tmp/candidate.patch"}}}
	got = item.OutputSealingRequest()
	item.DeclaredOutputs[0].Path = "/tmp/mutated.patch"
	if got.Outputs[0].GetPath() != "/tmp/candidate.patch" {
		t.Fatal("output sealing command aliases the reconcile projection")
	}
}
