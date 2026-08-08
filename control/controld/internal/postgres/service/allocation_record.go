package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

type allocationRecord struct {
	AllocationID      string
	OwnerType         string
	OwnerID           string
	DesiredSpecDigest string
	EnvironmentID     string
	NodeID            string
	NodeTarget        string
	Attempt           int64
	Status            commonv1.AllocationStatus
	Ready             bool
	ReadinessMessage  string
	ExitCode          int32
	ExitCodeKnown     bool
	Message           string
	CreatedAt         time.Time
	ReadinessProbe    *servicev1.ServiceProbe
	LivenessProbe     *servicev1.ServiceProbe
	Config            *commonv1.ExecutionConfig
}

func (s *PGStore) serviceAllocation(ctx context.Context, tx pgx.Tx, serviceID, allocationID string) (*servicekernel.AllocationRecord, error) {
	record := &servicekernel.AllocationRecord{}
	if err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target, a.attempt
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.owner_type = $1 AND a.owner_id = $2 AND a.allocation_id = $3
	`, allocationOwnerService, strings.TrimSpace(serviceID), strings.TrimSpace(allocationID)).Scan(&record.AllocationID, &record.NodeID, &record.NodeTarget, &record.Attempt); err != nil {
		return nil, err
	}
	record.ServiceID = strings.TrimSpace(serviceID)
	return record, nil
}

func (s *PGStore) allocationRecordsForStatusBatch(ctx context.Context, tx pgx.Tx, allocationIDs []string) (map[string]*allocationRecord, error) {
	records := make(map[string]*allocationRecord, len(allocationIDs))
	if len(allocationIDs) == 0 {
		return records, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT a.allocation_id, a.owner_type, a.owner_id, a.desired_spec_digest, a.environment_id, a.node_id, n.node_target, a.attempt, a.status, a.ready,
			a.readiness_message, a.exit_code, a.exit_code_known, a.message, a.created_at, a.readiness_probe, a.liveness_probe, a.config
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = ANY($1::text[])
		ORDER BY a.allocation_id
		FOR UPDATE OF a
	`, allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("lock service allocations for status batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanStatusAllocationRecord(rows)
		if err != nil {
			return nil, err
		}
		records[record.AllocationID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service allocations for status batch: %w", err)
	}
	return records, nil
}

type statusAllocationRecordScanner interface {
	Scan(dest ...any) error
}

func scanStatusAllocationRecord(row statusAllocationRecordScanner) (*allocationRecord, error) {
	record := &allocationRecord{}
	var statusText string
	var configJSON, readinessProbeJSON, livenessProbeJSON []byte
	if err := row.Scan(
		&record.AllocationID,
		&record.OwnerType,
		&record.OwnerID,
		&record.DesiredSpecDigest,
		&record.EnvironmentID,
		&record.NodeID,
		&record.NodeTarget,
		&record.Attempt,
		&statusText,
		&record.Ready,
		&record.ReadinessMessage,
		&record.ExitCode,
		&record.ExitCodeKnown,
		&record.Message,
		&record.CreatedAt,
		&readinessProbeJSON,
		&livenessProbeJSON,
		&configJSON,
	); err != nil {
		return nil, fmt.Errorf("scan service allocation for status batch: %w", err)
	}
	record.Status = allocationkernel.ParseStatus(statusText)
	if len(readinessProbeJSON) > 0 && string(readinessProbeJSON) != "null" {
		record.ReadinessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(readinessProbeJSON, record.ReadinessProbe); err != nil {
			return nil, fmt.Errorf("unmarshal allocation readiness probe: %w", err)
		}
		record.ReadinessProbe = servicekernel.NormalizeReadinessProbe(record.ReadinessProbe)
	}
	if len(livenessProbeJSON) > 0 && string(livenessProbeJSON) != "null" {
		record.LivenessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(livenessProbeJSON, record.LivenessProbe); err != nil {
			return nil, fmt.Errorf("unmarshal allocation liveness probe: %w", err)
		}
		record.LivenessProbe = servicekernel.NormalizeLivenessProbe(record.LivenessProbe)
	}
	record.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, record.Config); err != nil {
		return nil, fmt.Errorf("unmarshal allocation config: %w", err)
	}
	return record, nil
}
