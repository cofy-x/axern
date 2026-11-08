package output

import (
	"bytes"
	"encoding/json"
	"testing"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
)

func TestWorkspaceAndRunJSONKeepStableDataAndWriterIdentity(t *testing.T) {
	value := &workspacev1.Workspace{ID: "ws-a", Namespace: "team", NodeID: "node-a", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY, CapacityBytes: 4096, InodeLimit: 64, Generation: 2, AttachedAllocationID: "alloc-a"}
	var out bytes.Buffer
	if err := PrintWorkspaceResponseJSON(&out, value); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if ws := decoded["workspace"]; ws["id"] != "ws-a" || ws["status"] != "ready" || ws["attached_allocation_id"] != "alloc-a" || ws["capacity_bytes"] != float64(4096) {
		t.Fatalf("unexpected Workspace JSON: %s", out.String())
	}
	run := NewRunJSON(&runv1.Run{ID: "run-a", WorkspaceAttachment: &workspacev1.WorkspaceAttachment{WorkspaceID: "ws-a", NodeID: "node-a", Generation: 2, Target: "/workspace"}})
	if run.WorkspaceAttachment == nil || run.WorkspaceAttachment.Generation != 2 || run.WorkspaceAttachment.WorkspaceID != "ws-a" {
		t.Fatalf("Run attachment identity omitted: %+v", run)
	}
}
