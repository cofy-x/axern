package output

import (
	"io"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

func RenderAdminAuditEventTable(w io.Writer, events []*adminv1.AdminAuditEvent) {
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		rows = append(rows, []string{
			FormatProtoTimestamp(event.GetCreatedAt()),
			adminAuditOperationLabel(event.GetOperation()),
			adminAuditTargetTypeLabel(event.GetTargetType()),
			event.GetTargetID(),
			ShortMessage(event.GetOperatorReason(), 72),
		})
	}
	RenderTable(w, []string{"CREATED", "OPERATION", "TARGET", "TARGET_ID", "OPERATOR_REASON"}, rows)
}

func adminAuditOperationLabel(operation adminv1.AdminAuditOperation) string {
	return trimEnumPrefix(operation.String(), "ADMIN_AUDIT_OPERATION_")
}

func adminAuditTargetTypeLabel(targetType adminv1.AdminAuditTargetType) string {
	return trimEnumPrefix(targetType.String(), "ADMIN_AUDIT_TARGET_TYPE_")
}
