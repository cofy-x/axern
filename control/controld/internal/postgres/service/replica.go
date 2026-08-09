package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const recentEndedReplicaLimit = 5

type serviceReplicaRecord struct {
	replica           *servicev1.ServiceReplica
	desiredSpecDigest string
}

type serviceReplicaScanner interface {
	Scan(dest ...any) error
}

func (s *PGStore) GetReplica(ctx context.Context, serviceID, replicaID string) (*servicev1.ServiceReplica, bool, error) {
	service, ok, err := s.Get(ctx, serviceID)
	if err != nil || !ok {
		return nil, ok, err
	}
	record, err := scanServiceReplicaRecord(s.db.Pool().QueryRow(ctx, `
		SELECT a.allocation_id, a.owner_id, a.desired_spec_digest, a.node_id, a.attempt, a.status, a.ready, a.readiness_message, a.message, a.exit_code, a.exit_code_known, a.created_at, a.updated_at, a.workspace_preparation,
			COALESCE((SELECT revision FROM allocation_capability_condition_sets cs WHERE cs.allocation_id = a.allocation_id AND cs.allocation_attempt = a.attempt), 0),
			(SELECT observed_at FROM allocation_capability_condition_sets cs WHERE cs.allocation_id = a.allocation_id AND cs.allocation_attempt = a.attempt),
			COALESCE((SELECT jsonb_build_object('conditions', COALESCE(jsonb_agg(cc.condition ORDER BY cc.capability_key_id), '[]'::jsonb)) FROM allocation_capability_conditions cc WHERE cc.allocation_id = a.allocation_id AND cc.allocation_attempt = a.attempt), '{"conditions":[]}'::jsonb),
			COALESCE(q.reason, ''), COALESCE(q.reconcile_attempts, 0), COALESCE(q.last_error, ''), q.next_run_at
		FROM allocations a
		LEFT JOIN allocation_reconcile_queue q ON q.allocation_id = a.allocation_id
		WHERE a.owner_type = $1 AND a.owner_id = $2 AND a.allocation_id = $3
	`, allocationOwnerService, strings.TrimSpace(serviceID), strings.TrimSpace(replicaID)))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get service replica: %w", err)
	}
	return buildReplicaView(record, service), true, nil
}

func (s *PGStore) ListReplicas(ctx context.Context, serviceID string, filter *servicev1.ServiceReplicaListFilter) ([]*servicev1.ServiceReplica, error) {
	service, ok, err := s.Get(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT a.allocation_id, a.owner_id, a.desired_spec_digest, a.node_id, a.attempt, a.status, a.ready, a.readiness_message, a.message, a.exit_code, a.exit_code_known, a.created_at, a.updated_at, a.workspace_preparation,
			COALESCE((SELECT revision FROM allocation_capability_condition_sets cs WHERE cs.allocation_id = a.allocation_id AND cs.allocation_attempt = a.attempt), 0),
			(SELECT observed_at FROM allocation_capability_condition_sets cs WHERE cs.allocation_id = a.allocation_id AND cs.allocation_attempt = a.attempt),
			COALESCE((SELECT jsonb_build_object('conditions', COALESCE(jsonb_agg(cc.condition ORDER BY cc.capability_key_id), '[]'::jsonb)) FROM allocation_capability_conditions cc WHERE cc.allocation_id = a.allocation_id AND cc.allocation_attempt = a.attempt), '{"conditions":[]}'::jsonb),
			COALESCE(q.reason, ''), COALESCE(q.reconcile_attempts, 0), COALESCE(q.last_error, ''), q.next_run_at
		FROM allocations a
		LEFT JOIN allocation_reconcile_queue q ON q.allocation_id = a.allocation_id
		WHERE a.owner_type = $1 AND a.owner_id = $2
		ORDER BY a.updated_at DESC, a.created_at DESC, a.allocation_id DESC
	`, allocationOwnerService, strings.TrimSpace(serviceID))
	if err != nil {
		return nil, fmt.Errorf("list service replicas: %w", err)
	}
	defer rows.Close()

	current := make([]*servicev1.ServiceReplica, 0)
	ended := make([]*servicev1.ServiceReplica, 0, recentEndedReplicaLimit)
	for rows.Next() {
		record, err := scanServiceReplicaRecord(rows)
		if err != nil {
			return nil, err
		}
		replica := buildReplicaView(record, service)
		if !matchReplicaFilter(replica, filter) {
			continue
		}
		if replica.GetEnded() {
			if len(ended) < recentEndedReplicaLimit {
				ended = append(ended, replica)
			}
			continue
		}
		current = append(current, replica)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(current, ended...), nil
}

func scanServiceReplicaRecord(row serviceReplicaScanner) (*serviceReplicaRecord, error) {
	var (
		replica                     servicev1.ServiceReplica
		desiredSpecDigest           string
		statusText                  string
		retryReason, retryLastError string
		retryAttempts               int32
		retryNextRunAt              pgtype.Timestamptz
		createdAt, updatedAt        time.Time
		workspacePreparationJSON    []byte
		capabilityConditionsJSON    []byte
		capabilityRevision          int64
		capabilityObservedAt        pgtype.Timestamptz
	)
	if err := row.Scan(
		&replica.ID,
		&replica.ServiceID,
		&desiredSpecDigest,
		&replica.NodeID,
		&replica.Attempt,
		&statusText,
		&replica.Ready,
		&replica.ReadinessMessage,
		&replica.Message,
		&replica.ExitCode,
		&replica.ExitCodeKnown,
		&createdAt,
		&updatedAt,
		&workspacePreparationJSON,
		&capabilityRevision,
		&capabilityObservedAt,
		&capabilityConditionsJSON,
		&retryReason,
		&retryAttempts,
		&retryLastError,
		&retryNextRunAt,
	); err != nil {
		return nil, err
	}
	replica.Status = allocationkernel.ParseStatus(statusText)
	replica.Ended = allocationkernel.IsEnded(replica.GetStatus())
	replica.CreatedAt = timestamppb.New(createdAt)
	replica.UpdatedAt = timestamppb.New(updatedAt)
	if strings.TrimSpace(retryReason) != "" {
		replica.LifecycleRetry = &servicev1.ServiceReplicaLifecycleRetry{
			Reason:    serviceReplicaLifecycleRetryReason(retryReason),
			Attempts:  retryAttempts,
			LastError: strings.TrimSpace(retryLastError),
		}
		if retryNextRunAt.Valid {
			replica.LifecycleRetry.NextRunAt = timestamppb.New(retryNextRunAt.Time)
		}
	}
	if len(workspacePreparationJSON) > 0 && string(workspacePreparationJSON) != "null" {
		replica.WorkspacePreparation = &commonv1.WorkspacePreparationFacts{}
		if err := protojson.Unmarshal(workspacePreparationJSON, replica.WorkspacePreparation); err != nil {
			return nil, fmt.Errorf("unmarshal workspace preparation: %w", err)
		}
	}
	conditions := &capabilityv1.CapabilityConditionSet{}
	if err := protojson.Unmarshal(capabilityConditionsJSON, conditions); err != nil {
		return nil, fmt.Errorf("unmarshal replica capability conditions: %w", err)
	}
	if capabilityRevision > 0 && capabilityObservedAt.Valid {
		conditions.Revision = capabilityRevision
		conditions.ObservedAt = timestamppb.New(capabilityObservedAt.Time)
		replica.CapabilityConditions = conditions
	}
	return &serviceReplicaRecord{
		replica:           &replica,
		desiredSpecDigest: desiredSpecDigest,
	}, nil
}

func serviceReplicaLifecycleRetryReason(reason string) servicev1.ServiceReplicaLifecycleRetryReason {
	switch strings.TrimSpace(reason) {
	case allocationkernel.ReconcileReasonCreate:
		return servicev1.ServiceReplicaLifecycleRetryReason_SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_CREATE
	case allocationkernel.ReconcileReasonDelete:
		return servicev1.ServiceReplicaLifecycleRetryReason_SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_DELETE
	default:
		return servicev1.ServiceReplicaLifecycleRetryReason_SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_UNSPECIFIED
	}
}
