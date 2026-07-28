package output

import (
	"io"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type ServiceEventListJSON struct {
	Events []*ServiceEventJSON `json:"events"`
}

type ServiceEventJSON struct {
	ID             string `json:"id"`
	ServiceID      string `json:"service_id"`
	ReplicaID      string `json:"replica_id,omitempty"`
	Type           string `json:"type"`
	Phase          string `json:"phase"`
	DiagnosticCode string `json:"diagnostic_code"`
	Message        string `json:"message,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func PrintServiceEventListJSON(w io.Writer, resp *servicev1.ListServiceEventsResponse) error {
	out := ServiceEventListJSON{}
	if resp != nil {
		out.Events = make([]*ServiceEventJSON, 0, len(resp.GetEvents()))
		for _, event := range resp.GetEvents() {
			out.Events = append(out.Events, NewServiceEventJSON(event))
		}
	}
	return PrintJSON(w, out)
}

func NewServiceEventJSON(event *servicev1.ServiceEvent) *ServiceEventJSON {
	if event == nil {
		return nil
	}
	return &ServiceEventJSON{
		ID:             event.GetID(),
		ServiceID:      event.GetServiceID(),
		ReplicaID:      event.GetReplicaID(),
		Type:           ServiceEventTypeLabel(event.GetType()),
		Phase:          ServiceRolloutPhaseLabel(event.GetPhase()),
		DiagnosticCode: WorkloadDiagnosticCodeLabel(event.GetDiagnosticCode()),
		Message:        event.GetMessage(),
		CreatedAt:      FormatProtoTimestamp(event.GetCreatedAt()),
	}
}
