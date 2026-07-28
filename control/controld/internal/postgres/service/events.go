package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *PGStore) RecordEvent(ctx context.Context, event *servicev1.ServiceEvent) error {
	if event == nil || strings.TrimSpace(event.GetServiceID()) == "" {
		return nil
	}
	return recordServiceEvent(ctx, s.db.Pool(), event)
}

func (s *PGStore) ListEvents(ctx context.Context, serviceID string, limit int32) ([]*servicev1.ServiceEvent, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT event_id, service_id, replica_id, event_type, phase, diagnostic_code, message, created_at
		FROM service_events
		WHERE service_id = $1
		ORDER BY created_at DESC, event_id DESC
		LIMIT $2
	`, strings.TrimSpace(serviceID), servicekernel.NormalizeServiceEventLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list service events: %w", err)
	}
	defer rows.Close()
	out := make([]*servicev1.ServiceEvent, 0)
	for rows.Next() {
		event, err := scanServiceEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func recordServiceEvent(ctx context.Context, execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, event *servicev1.ServiceEvent) error {
	if event == nil || strings.TrimSpace(event.GetServiceID()) == "" {
		return nil
	}
	if _, err := execer.Exec(ctx, `
		INSERT INTO service_events (event_id, service_id, replica_id, event_type, phase, diagnostic_code, message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, event.GetID(), strings.TrimSpace(event.GetServiceID()), strings.TrimSpace(event.GetReplicaID()), event.GetType().String(), event.GetPhase().String(), event.GetDiagnosticCode().String(), strings.TrimSpace(event.GetMessage()), event.GetCreatedAt().AsTime().UTC()); err != nil {
		return fmt.Errorf("record service event: %w", err)
	}
	return nil
}

type serviceEventScanner interface {
	Scan(dest ...any) error
}

func scanServiceEvent(row serviceEventScanner) (*servicev1.ServiceEvent, error) {
	var (
		event     servicev1.ServiceEvent
		typeText  string
		phaseText string
		diagText  string
		createdAt time.Time
	)
	if err := row.Scan(&event.ID, &event.ServiceID, &event.ReplicaID, &typeText, &phaseText, &diagText, &event.Message, &createdAt); err != nil {
		return nil, err
	}
	event.Type = parseServiceEventType(typeText)
	event.Phase = parseServiceRolloutPhase(phaseText)
	event.DiagnosticCode = parseWorkloadDiagnosticCode(diagText)
	event.CreatedAt = timestamppb.New(createdAt)
	return &event, nil
}

func parseServiceEventType(value string) servicev1.ServiceEventType {
	if n, ok := servicev1.ServiceEventType_value[value]; ok {
		return servicev1.ServiceEventType(n)
	}
	return servicev1.ServiceEventType_SERVICE_EVENT_TYPE_UNSPECIFIED
}

func parseServiceRolloutPhase(value string) servicev1.ServiceRolloutPhase {
	if n, ok := servicev1.ServiceRolloutPhase_value[value]; ok {
		return servicev1.ServiceRolloutPhase(n)
	}
	return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED
}

func parseWorkloadDiagnosticCode(value string) commonv1.WorkloadDiagnosticCode {
	if n, ok := commonv1.WorkloadDiagnosticCode_value[value]; ok {
		return commonv1.WorkloadDiagnosticCode(n)
	}
	return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
}
