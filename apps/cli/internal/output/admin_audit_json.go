package output

import (
	"io"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

type AdminAuditEventListJSON struct {
	Events []*AdminAuditEventJSON `json:"events"`
}

type AdminAuditEventJSON struct {
	EventID          string `json:"event_id"`
	Operation        string `json:"operation"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	OperatorReason   string `json:"operator_reason"`
	ActorPrincipalID string `json:"actor_principal_id,omitempty"`
	CreatedAt        string `json:"created_at"`
}

func PrintAdminAuditEventListJSON(w io.Writer, events []*adminv1.AdminAuditEvent) error {
	out := AdminAuditEventListJSON{Events: make([]*AdminAuditEventJSON, 0, len(events))}
	for _, event := range events {
		out.Events = append(out.Events, NewAdminAuditEventJSON(event))
	}
	return PrintJSON(w, out)
}

func NewAdminAuditEventJSON(event *adminv1.AdminAuditEvent) *AdminAuditEventJSON {
	if event == nil {
		return nil
	}
	return &AdminAuditEventJSON{
		EventID:          event.GetEventID(),
		Operation:        adminAuditOperationLabel(event.GetOperation()),
		TargetType:       adminAuditTargetTypeLabel(event.GetTargetType()),
		TargetID:         event.GetTargetID(),
		OperatorReason:   event.GetOperatorReason(),
		ActorPrincipalID: event.GetActorPrincipalID(),
		CreatedAt:        FormatProtoTimestamp(event.GetCreatedAt()),
	}
}
