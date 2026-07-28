package output

import (
	"fmt"
	"io"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func RenderServiceLatestEvent(w io.Writer, event *servicev1.ServiceEvent) {
	if event == nil {
		return
	}
	parts := []string{ServiceEventTypeLabel(event.GetType())}
	if event.GetPhase() != servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED {
		parts = append(parts, fmt.Sprintf("phase=%s", ServiceRolloutPhaseLabel(event.GetPhase())))
	}
	if event.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		parts = append(parts, fmt.Sprintf("diagnostic=%s", WorkloadDiagnosticCodeLabel(event.GetDiagnosticCode())))
	}
	if event.GetReplicaID() != "" {
		parts = append(parts, fmt.Sprintf("replica=%s", event.GetReplicaID()))
	}
	if event.GetMessage() != "" {
		parts = append(parts, fmt.Sprintf("message=%s", ShortMessage(event.GetMessage(), 80)))
	}
	fmt.Fprintf(w, "Latest Event: %s\n", strings.Join(parts, " "))
}

func RenderServiceEventTable(w io.Writer, events []*servicev1.ServiceEvent) {
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		rows = append(rows, []string{
			FormatProtoTimestamp(event.GetCreatedAt()),
			ServiceEventTypeLabel(event.GetType()),
			ServiceRolloutPhaseLabel(event.GetPhase()),
			WorkloadDiagnosticCodeLabel(event.GetDiagnosticCode()),
			firstNonEmpty(event.GetReplicaID(), "-"),
			ShortMessage(event.GetMessage(), 64),
		})
	}
	RenderTable(w, []string{"CREATED_AT", "TYPE", "PHASE", "DIAGNOSTIC", "REPLICA", "MESSAGE"}, rows)
}
