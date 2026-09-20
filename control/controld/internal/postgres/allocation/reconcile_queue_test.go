package pgallocation

import (
	"testing"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestRootfsSnapshotBaseImageRefUsesFrozenRunSource(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := rootfsSnapshotBaseImageRef("docker.io/library/python:3.12-slim", digest)
	if err != nil {
		t.Fatalf("rootfsSnapshotBaseImageRef() error = %v", err)
	}
	if want := "docker.io/library/python@" + digest; got != want {
		t.Fatalf("rootfsSnapshotBaseImageRef() = %q, want %q", got, want)
	}
}

func TestRootfsSnapshotBaseImageRefRejectsIncompleteFrozenContract(t *testing.T) {
	for _, test := range []struct {
		name   string
		ref    string
		digest string
	}{
		{name: "missing source", digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "missing digest", ref: "docker.io/library/python:3.12-slim"},
		{name: "non sha256 digest", ref: "docker.io/library/python:3.12-slim", digest: "sha512:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := rootfsSnapshotBaseImageRef(test.ref, test.digest); err == nil {
				t.Fatal("rootfsSnapshotBaseImageRef() accepted an incomplete contract")
			}
		})
	}
}

func TestDecodeDeclaredOutputsPreservesCleanupContract(t *testing.T) {
	item := &allocationkernel.ReconcileItem{AllocationID: "allocation-output"}
	if err := decodeDeclaredOutputs([]byte(`[{"path":"/tmp/output.patch","format":"DECLARED_OUTPUT_FORMAT_FILE","mediaType":"text/x-diff"}]`), item); err != nil {
		t.Fatal(err)
	}
	if got := item.DeclaredOutputs; len(got) != 1 || got[0].GetPath() != "/tmp/output.patch" || got[0].GetFormat() != commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE || got[0].GetMediaType() != "text/x-diff" {
		t.Fatalf("declared outputs = %#v", got)
	}
}

func TestDecodeDeclaredOutputsRejectsInvalidPersistedContract(t *testing.T) {
	item := &allocationkernel.ReconcileItem{AllocationID: "allocation-output"}
	if err := decodeDeclaredOutputs([]byte(`[{"path":42}]`), item); err == nil {
		t.Fatal("decodeDeclaredOutputs() accepted invalid persisted JSON")
	}
}
