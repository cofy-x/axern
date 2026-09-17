package pgallocation

import (
	"testing"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestDecodeDeclaredOutputsPreservesCleanupContract(t *testing.T) {
	item := &allocationkernel.ReconcileItem{AllocationID: "allocation-output"}
	if err := decodeDeclaredOutputs([]byte(`[{"path":"/tmp/candidate.patch","format":"DECLARED_OUTPUT_FORMAT_FILE","mediaType":"text/x-diff"}]`), item); err != nil {
		t.Fatal(err)
	}
	if got := item.DeclaredOutputs; len(got) != 1 || got[0].GetPath() != "/tmp/candidate.patch" || got[0].GetFormat() != commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE || got[0].GetMediaType() != "text/x-diff" {
		t.Fatalf("declared outputs = %#v", got)
	}
}

func TestDecodeDeclaredOutputsRejectsInvalidPersistedContract(t *testing.T) {
	item := &allocationkernel.ReconcileItem{AllocationID: "allocation-output"}
	if err := decodeDeclaredOutputs([]byte(`[{"path":42}]`), item); err == nil {
		t.Fatal("decodeDeclaredOutputs() accepted invalid persisted JSON")
	}
}
