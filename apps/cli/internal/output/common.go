package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func PrintJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func PrintProtoJSON(w io.Writer, msg interface{ ProtoReflect() protoreflect.Message }) error {
	encoded, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func FormatProtoTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func FormatRelativeAge(from, to time.Time) string {
	if from.IsZero() {
		return "-"
	}
	if to.Before(from) {
		from, to = to, from
	}
	d := to.Sub(from)
	switch {
	case d < time.Minute:
		return "just-now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

func ServiceStatusLabel(status servicev1.ServiceStatus) string {
	return trimEnumPrefix(status.String(), "SERVICE_STATUS_")
}

func AllocationStatusLabel(status commonv1.AllocationStatus) string {
	return trimEnumPrefix(status.String(), "ALLOCATION_STATUS_")
}

func RunStatusLabel(status runv1.RunStatus) string {
	return trimEnumPrefix(status.String(), "RUN_STATUS_")
}

func EnvironmentStatusLabel(status environmentv1.EnvironmentStatus) string {
	return trimEnumPrefix(status.String(), "ENVIRONMENT_STATUS_")
}

func SecretTypeLabel(secretType secretv1.SecretType) string {
	return trimEnumPrefix(secretType.String(), "SECRET_TYPE_")
}

func WorkloadDiagnosticCodeLabel(code commonv1.WorkloadDiagnosticCode) string {
	return trimEnumPrefix(code.String(), "WORKLOAD_DIAGNOSTIC_CODE_")
}

func ServiceRolloutPhaseLabel(phase servicev1.ServiceRolloutPhase) string {
	return trimEnumPrefix(phase.String(), "SERVICE_ROLLOUT_PHASE_")
}

func ServiceEventTypeLabel(eventType servicev1.ServiceEventType) string {
	return trimEnumPrefix(eventType.String(), "SERVICE_EVENT_TYPE_")
}

func ServiceAutoscalingActionLabel(action servicev1.ServiceAutoscalingAction) string {
	return trimEnumPrefix(action.String(), "SERVICE_AUTOSCALING_ACTION_")
}

func ShortMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func trimEnumPrefix(value string, prefix string) string {
	value = strings.TrimPrefix(value, prefix)
	return strings.ToLower(strings.ReplaceAll(value, "_", "-"))
}
