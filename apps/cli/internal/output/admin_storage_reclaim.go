package output

import (
	"fmt"
	"io"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

type StorageReclaimJSON struct {
	ClaimID     string `json:"claim_id"`
	Namespace   string `json:"namespace"`
	ClaimName   string `json:"claim_name"`
	ServiceID   string `json:"service_id"`
	NodeID      string `json:"node_id"`
	Attempt     int64  `json:"attempt"`
	NextRetryAt string `json:"next_retry_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func PrintStorageReclaimListJSON(w io.Writer, reclaims []*adminv1.StorageReclaim) error {
	out := make([]StorageReclaimJSON, 0, len(reclaims))
	for _, reclaim := range reclaims {
		if reclaim == nil {
			continue
		}
		out = append(out, StorageReclaimJSON{
			ClaimID: reclaim.GetClaimID(), Namespace: reclaim.GetNamespace(), ClaimName: reclaim.GetClaimName(),
			ServiceID: reclaim.GetServiceID(), NodeID: reclaim.GetNodeID(), Attempt: reclaim.GetAttempt(),
			NextRetryAt: FormatProtoTimestamp(reclaim.GetNextRetryAt()), LastError: reclaim.GetLastError(), UpdatedAt: FormatProtoTimestamp(reclaim.GetUpdatedAt()),
		})
	}
	return PrintJSON(w, map[string]any{"reclaims": out})
}

func RenderStorageReclaimTable(w io.Writer, reclaims []*adminv1.StorageReclaim) {
	rows := make([][]string, 0, len(reclaims))
	for _, reclaim := range reclaims {
		if reclaim == nil {
			continue
		}
		rows = append(rows, []string{
			reclaim.GetClaimID(), reclaim.GetNamespace(), reclaim.GetClaimName(), reclaim.GetServiceID(), reclaim.GetNodeID(),
			formatInt(reclaim.GetAttempt()), FormatProtoTimestamp(reclaim.GetNextRetryAt()), FormatProtoTimestamp(reclaim.GetUpdatedAt()), ShortMessage(reclaim.GetLastError(), 48),
		})
	}
	RenderTable(w, []string{"CLAIM ID", "NAMESPACE", "CLAIM", "SERVICE", "NODE", "ATTEMPT", "NEXT RETRY", "UPDATED", "LAST ERROR"}, rows)
}

func formatInt(value int64) string {
	if value == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", value)
}
