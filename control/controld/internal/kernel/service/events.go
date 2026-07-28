package servicekernel

import (
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultServiceEventLimit = 50

func newServiceEvent(serviceID, replicaID string, eventType servicev1.ServiceEventType, phase servicev1.ServiceRolloutPhase, diagnosticCode commonv1.WorkloadDiagnosticCode, message string, now time.Time) *servicev1.ServiceEvent {
	return &servicev1.ServiceEvent{
		ID:             "svcevt-" + uuid.NewString(),
		ServiceID:      strings.TrimSpace(serviceID),
		ReplicaID:      strings.TrimSpace(replicaID),
		Type:           eventType,
		Phase:          phase,
		DiagnosticCode: diagnosticCode,
		Message:        strings.TrimSpace(message),
		CreatedAt:      timestamppb.New(now.UTC()),
	}
}

func NewServiceEvent(serviceID, replicaID string, eventType servicev1.ServiceEventType, phase servicev1.ServiceRolloutPhase, diagnosticCode commonv1.WorkloadDiagnosticCode, message string, now time.Time) *servicev1.ServiceEvent {
	return newServiceEvent(serviceID, replicaID, eventType, phase, diagnosticCode, message, now)
}

func normalizeServiceEventLimit(limit int32) int32 {
	if limit <= 0 || limit > defaultServiceEventLimit {
		return defaultServiceEventLimit
	}
	return limit
}

func NormalizeServiceEventLimit(limit int32) int32 {
	return normalizeServiceEventLimit(limit)
}
