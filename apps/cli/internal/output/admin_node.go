package output

import (
	"io"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

type AdminNodeJSON struct {
	NodeID              string `json:"node_id"`
	LifecycleStatus     string `json:"lifecycle_status"`
	HeartbeatFresh      bool   `json:"heartbeat_fresh"`
	SummaryFresh        bool   `json:"summary_fresh"`
	AxnodedReady        bool   `json:"axnoded_ready"`
	HeartbeatAgeSeconds int64  `json:"heartbeat_age_seconds"`
	SummaryAgeSeconds   int64  `json:"summary_age_seconds"`
	RegisteredAt        string `json:"registered_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
	RetiredAt           string `json:"retired_at,omitempty"`
	RetiredReason       string `json:"retired_reason,omitempty"`
}

func RenderAdminNodeTable(w io.Writer, nodes []*adminv1.AdminNode) {
	rows := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, adminNodeRow(node))
	}
	RenderTable(w, []string{"NODE", "STATUS", "HEARTBEAT", "SUMMARY", "READY", "UPDATED", "RETIRED", "REASON"}, rows)
}

func RenderAdminNode(w io.Writer, node *adminv1.AdminNode) {
	RenderAdminNodeTable(w, []*adminv1.AdminNode{node})
}

func adminNodeRow(node *adminv1.AdminNode) []string {
	if node == nil {
		return make([]string, 8)
	}
	return []string{node.GetNodeID(), adminNodeLifecycleLabel(node.GetLifecycleStatus()), boolLabel(node.GetHeartbeatFresh()), boolLabel(node.GetSummaryFresh()), boolLabel(node.GetAxnodedReady()), FormatProtoTimestamp(node.GetUpdatedAt()), FormatProtoTimestamp(node.GetRetiredAt()), ShortMessage(node.GetRetiredReason(), 64)}
}

func PrintAdminNodeListJSON(w io.Writer, nodes []*adminv1.AdminNode) error {
	out := make([]*AdminNodeJSON, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, NewAdminNodeJSON(node))
	}
	return PrintJSON(w, struct {
		Nodes []*AdminNodeJSON `json:"nodes"`
	}{Nodes: out})
}

func PrintAdminNodeJSON(w io.Writer, node *adminv1.AdminNode) error {
	return PrintJSON(w, NewAdminNodeJSON(node))
}

func NewAdminNodeJSON(node *adminv1.AdminNode) *AdminNodeJSON {
	if node == nil {
		return nil
	}
	return &AdminNodeJSON{NodeID: node.GetNodeID(), LifecycleStatus: adminNodeLifecycleLabel(node.GetLifecycleStatus()), HeartbeatFresh: node.GetHeartbeatFresh(), SummaryFresh: node.GetSummaryFresh(), AxnodedReady: node.GetAxnodedReady(), HeartbeatAgeSeconds: node.GetHeartbeatAgeSeconds(), SummaryAgeSeconds: node.GetSummaryAgeSeconds(), RegisteredAt: FormatProtoTimestamp(node.GetRegisteredAt()), UpdatedAt: FormatProtoTimestamp(node.GetUpdatedAt()), RetiredAt: FormatProtoTimestamp(node.GetRetiredAt()), RetiredReason: node.GetRetiredReason()}
}

func adminNodeLifecycleLabel(status adminv1.AdminNodeLifecycleStatus) string {
	return strings.ToLower(trimEnumPrefix(status.String(), "ADMIN_NODE_LIFECYCLE_STATUS_"))
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
