package output

import (
	"fmt"
	"io"
	"strings"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
)

type WorkspaceJSON struct {
	ID                   string            `json:"id"`
	Namespace            string            `json:"namespace"`
	NodeID               string            `json:"node_id"`
	Status               string            `json:"status"`
	CapacityBytes        int64             `json:"capacity_bytes"`
	InodeLimit           int64             `json:"inode_limit"`
	Generation           int64             `json:"generation"`
	AttachedAllocationID string            `json:"attached_allocation_id,omitempty"`
	UsedBytes            int64             `json:"used_bytes"`
	UsedInodes           int64             `json:"used_inodes"`
	Message              string            `json:"message,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	CreatedAt            string            `json:"created_at,omitempty"`
	UpdatedAt            string            `json:"updated_at,omitempty"`
}

type WorkspaceResponseJSON struct {
	Workspace *WorkspaceJSON `json:"workspace"`
}
type WorkspaceListJSON struct {
	Workspaces []*WorkspaceJSON `json:"workspaces"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func NewWorkspaceJSON(value *workspacev1.Workspace) *WorkspaceJSON {
	if value == nil {
		return nil
	}
	return &WorkspaceJSON{ID: value.GetID(), Namespace: value.GetNamespace(), NodeID: value.GetNodeID(), Status: workspaceStatus(value.GetStatus()), CapacityBytes: value.GetCapacityBytes(), InodeLimit: value.GetInodeLimit(), Generation: value.GetGeneration(), AttachedAllocationID: value.GetAttachedAllocationID(), UsedBytes: value.GetUsedBytes(), UsedInodes: value.GetUsedInodes(), Message: value.GetMessage(), Labels: cloneStringMap(value.GetLabels()), CreatedAt: FormatProtoTimestamp(value.GetCreatedAt()), UpdatedAt: FormatProtoTimestamp(value.GetUpdatedAt())}
}

func PrintWorkspaceResponseJSON(w io.Writer, value *workspacev1.Workspace) error {
	return PrintJSON(w, WorkspaceResponseJSON{Workspace: NewWorkspaceJSON(value)})
}
func PrintWorkspaceListJSON(w io.Writer, response *workspacev1.ListWorkspacesResponse) error {
	out := WorkspaceListJSON{Workspaces: make([]*WorkspaceJSON, 0, len(response.GetWorkspaces())), NextCursor: response.GetNextCursor()}
	for _, value := range response.GetWorkspaces() {
		out.Workspaces = append(out.Workspaces, NewWorkspaceJSON(value))
	}
	return PrintJSON(w, out)
}

func RenderWorkspace(w io.Writer, value *workspacev1.Workspace) {
	if value == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\nNamespace: %s\nNode ID: %s\nStatus: %s\n", value.GetID(), value.GetNamespace(), value.GetNodeID(), workspaceStatus(value.GetStatus()))
	fmt.Fprintf(w, "Capacity: %d bytes\nInode Limit: %d\nUsed: %d bytes / %d inodes\nGeneration: %d\n", value.GetCapacityBytes(), value.GetInodeLimit(), value.GetUsedBytes(), value.GetUsedInodes(), value.GetGeneration())
	if value.GetAttachedAllocationID() != "" {
		fmt.Fprintf(w, "Attached Allocation ID: %s\n", value.GetAttachedAllocationID())
	}
	if value.GetMessage() != "" {
		fmt.Fprintf(w, "Message: %s\n", value.GetMessage())
	}
}

func RenderWorkspaceTable(w io.Writer, values []*workspacev1.Workspace) {
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		rows = append(rows, []string{value.GetID(), value.GetNamespace(), value.GetNodeID(), workspaceStatus(value.GetStatus()), fmt.Sprint(value.GetCapacityBytes()), fmt.Sprint(value.GetInodeLimit()), firstNonEmpty(value.GetAttachedAllocationID(), "-")})
	}
	RenderTable(w, []string{"ID", "NAMESPACE", "NODE", "STATUS", "CAPACITY BYTES", "INODE LIMIT", "ALLOCATION"}, rows)
}

func workspaceStatus(value workspacev1.WorkspaceStatus) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "WORKSPACE_STATUS_"))
}
