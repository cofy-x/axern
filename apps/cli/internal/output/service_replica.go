package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RenderServiceReplicaTable(w io.Writer, replicas []*servicev1.ServiceReplica) {
	cols := serviceReplicaColumns(replicas)
	rows := make([][]string, 0, len(replicas))
	for _, replica := range replicas {
		if replica == nil {
			continue
		}
		values := []string{
			replica.GetID(),
			AllocationStatusLabel(replica.GetStatus()),
			fmt.Sprintf("%t", replica.GetReady()),
			replica.GetNodeID(),
			fmt.Sprintf("%d", replica.GetAttempt()),
			formatReplicaUpdated(replica.GetUpdatedAt()),
		}
		if cols.ended {
			values = append(values, fmt.Sprintf("%t", replica.GetEnded()))
		}
		if cols.outdated {
			values = append(values, fmt.Sprintf("%t", replica.GetOutdated()))
		}
		if cols.diagnostic {
			values = append(values, WorkloadDiagnosticCodeLabel(replica.GetDiagnosticCode()))
		}
		if cols.retry {
			values = append(values, formatReplicaRetry(replica.GetLifecycleRetry()))
		}
		if cols.readinessMessage {
			values = append(values, ShortMessage(replica.GetReadinessMessage(), 32))
		}
		if cols.message {
			values = append(values, ShortMessage(replica.GetMessage(), 48))
		}
		rows = append(rows, values)
	}
	RenderTable(w, cols.headers(), rows)
}

type serviceReplicaTableColumns struct {
	ended            bool
	outdated         bool
	diagnostic       bool
	retry            bool
	readinessMessage bool
	message          bool
}

func (c serviceReplicaTableColumns) headers() []string {
	headers := []string{"ID", "STATUS", "READY", "NODE", "ATTEMPT", "UPDATED"}
	if c.ended {
		headers = append(headers, "ENDED")
	}
	if c.outdated {
		headers = append(headers, "OUTDATED")
	}
	if c.diagnostic {
		headers = append(headers, "DIAGNOSTIC")
	}
	if c.retry {
		headers = append(headers, "RETRY")
	}
	if c.readinessMessage {
		headers = append(headers, "READINESS")
	}
	if c.message {
		headers = append(headers, "MESSAGE")
	}
	return headers
}

func serviceReplicaColumns(replicas []*servicev1.ServiceReplica) serviceReplicaTableColumns {
	var cols serviceReplicaTableColumns
	for _, replica := range replicas {
		if replica == nil {
			continue
		}
		cols.ended = cols.ended || replica.GetEnded()
		cols.outdated = cols.outdated || replica.GetOutdated()
		cols.diagnostic = cols.diagnostic || replica.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
		cols.retry = cols.retry || replica.GetLifecycleRetry() != nil
		cols.readinessMessage = cols.readinessMessage || strings.TrimSpace(replica.GetReadinessMessage()) != ""
		cols.message = cols.message || strings.TrimSpace(replica.GetMessage()) != ""
	}
	return cols
}

func formatReplicaRetry(retry *servicev1.ServiceReplicaLifecycleRetry) string {
	if retry == nil {
		return ""
	}
	parts := []string{serviceReplicaLifecycleRetryReasonLabel(retry.GetReason()), fmt.Sprintf("attempts=%d", retry.GetAttempts())}
	if retry.GetNextRunAt() != nil {
		parts = append(parts, "next="+FormatRelativeAge(retry.GetNextRunAt().AsTime(), time.Now().UTC()))
	}
	if strings.TrimSpace(retry.GetLastError()) != "" {
		parts = append(parts, ShortMessage(retry.GetLastError(), 32))
	}
	return strings.Join(parts, " ")
}

func serviceReplicaLifecycleRetryReasonLabel(reason servicev1.ServiceReplicaLifecycleRetryReason) string {
	label := strings.TrimPrefix(strings.ToLower(reason.String()), "service_replica_lifecycle_retry_reason_")
	if label == "" || label == "unspecified" {
		return "pending"
	}
	return label
}

func formatReplicaUpdated(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	return FormatRelativeAge(ts.AsTime(), time.Now().UTC())
}
