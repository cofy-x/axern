package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

func RenderAllocationLifecycleRetry(w io.Writer, retry *adminv1.AllocationLifecycleRetry) {
	if retry == nil {
		return
	}
	fmt.Fprintf(w, "Allocation: %s\n", retry.GetAllocationID())
	fmt.Fprintf(w, "Owner: %s/%s\n", allocationLifecycleRetryOwnerLabel(retry.GetOwnerType()), retry.GetOwnerID())
	fmt.Fprintf(w, "Reason: %s\n", allocationLifecycleRetryReasonLabel(retry.GetReason()))
	fmt.Fprintf(w, "Node: %s\n", retry.GetNodeID())
	if retry.GetNodeTarget() != "" {
		fmt.Fprintf(w, "Node Target: %s\n", retry.GetNodeTarget())
	}
	fmt.Fprintf(w, "Attempt: %d\n", retry.GetAttempt())
	fmt.Fprintf(w, "Reconcile Attempts: %d\n", retry.GetReconcileAttempts())
	if retry.GetLastError() != "" {
		fmt.Fprintf(w, "Last Error: %s\n", retry.GetLastError())
	}
	fmt.Fprintf(w, "Next Run At: %s\n", FormatProtoTimestamp(retry.GetNextRunAt()))
	fmt.Fprintf(w, "Due: %t\n", retry.GetDue())
	fmt.Fprintf(w, "Created At: %s\n", FormatProtoTimestamp(retry.GetCreatedAt()))
	fmt.Fprintf(w, "Updated At: %s\n", FormatProtoTimestamp(retry.GetUpdatedAt()))
}

func RenderAllocationLifecycleRetryTable(w io.Writer, retries []*adminv1.AllocationLifecycleRetry) {
	rows := make([][]string, 0, len(retries))
	now := time.Now().UTC()
	for _, retry := range retries {
		if retry == nil {
			continue
		}
		rows = append(rows, []string{
			retry.GetAllocationID(),
			allocationLifecycleRetryOwnerLabel(retry.GetOwnerType()),
			retry.GetOwnerID(),
			allocationLifecycleRetryReasonLabel(retry.GetReason()),
			retry.GetNodeID(),
			fmt.Sprintf("%d", retry.GetAttempt()),
			fmt.Sprintf("%d", retry.GetReconcileAttempts()),
			formatAllocationLifecycleRetryNextRun(retry, now),
			fmt.Sprintf("%t", retry.GetDue()),
			ShortMessage(retry.GetLastError(), 48),
		})
	}
	RenderTable(w, []string{"ALLOCATION", "OWNER", "OWNER_ID", "REASON", "NODE", "ATTEMPT", "RETRIES", "NEXT", "DUE", "LAST_ERROR"}, rows)
}

func formatAllocationLifecycleRetryNextRun(retry *adminv1.AllocationLifecycleRetry, now time.Time) string {
	if retry == nil || retry.GetNextRunAt() == nil {
		return "-"
	}
	return FormatRelativeAge(retry.GetNextRunAt().AsTime(), now)
}

func allocationLifecycleRetryOwnerLabel(ownerType adminv1.AllocationLifecycleRetryOwnerType) string {
	return strings.ToLower(trimEnumPrefix(ownerType.String(), "ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_"))
}

func allocationLifecycleRetryReasonLabel(reason adminv1.AllocationLifecycleRetryReason) string {
	return strings.ToLower(trimEnumPrefix(reason.String(), "ALLOCATION_LIFECYCLE_RETRY_REASON_"))
}
