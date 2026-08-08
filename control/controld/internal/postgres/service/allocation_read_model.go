package pgservice

import (
	"context"
	"fmt"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const allocationRecordSelectColumns = `a.allocation_id, a.desired_spec_digest, a.environment_id, a.node_id, n.node_target, a.attempt, a.status, a.ready, a.readiness_message, a.readiness_probe, a.liveness_probe, a.config`

const allocationRecordSelectColumnsWithServiceID = `a.owner_id, ` + allocationRecordSelectColumns

const allocationRecordSelectColumnsWithoutNodeTarget = `a.allocation_id, a.desired_spec_digest, a.environment_id, a.node_id, ''::text AS node_target, a.attempt, a.status, a.ready, a.readiness_message, a.readiness_probe, a.liveness_probe, a.config`

func (s *PGStore) listServiceAllocationsByServiceIDs(ctx context.Context, serviceIDs []string) (map[string][]*servicekernel.AllocationRecord, error) {
	serviceIDs = normalizedIDs(serviceIDs)
	if len(serviceIDs) == 0 {
		return map[string][]*servicekernel.AllocationRecord{}, nil
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT `+allocationRecordSelectColumnsWithServiceID+`
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.owner_type = $1 AND a.owner_id = ANY($2) AND a.status NOT IN ($3, $4)
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationOwnerService, serviceIDs, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String())
	if err != nil {
		return nil, fmt.Errorf("query service allocations by service ids: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]*servicekernel.AllocationRecord, len(serviceIDs))
	for rows.Next() {
		serviceID, record, err := scanAllocationRecordWithServiceID(rows)
		if err != nil {
			return nil, err
		}
		out[serviceID] = append(out[serviceID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type allocationRecordScanner interface {
	Scan(dest ...any) error
}

func scanAllocationRecord(row allocationRecordScanner) (*servicekernel.AllocationRecord, error) {
	var (
		record             servicekernel.AllocationRecord
		statusText         string
		configJSON         []byte
		readinessProbeJSON []byte
		livenessProbeJSON  []byte
	)
	if err := row.Scan(&record.AllocationID, &record.DesiredSpecDigest, &record.EnvironmentID, &record.NodeID, &record.NodeTarget, &record.Attempt, &statusText, &record.Ready, &record.ReadinessMessage, &readinessProbeJSON, &livenessProbeJSON, &configJSON); err != nil {
		return nil, err
	}
	if err := hydrateAllocationRecord(&record, statusText, readinessProbeJSON, livenessProbeJSON, configJSON); err != nil {
		return nil, err
	}
	return &record, nil
}

func scanAllocationRecordWithServiceID(row allocationRecordScanner) (string, *servicekernel.AllocationRecord, error) {
	var (
		serviceID          string
		record             servicekernel.AllocationRecord
		statusText         string
		configJSON         []byte
		readinessProbeJSON []byte
		livenessProbeJSON  []byte
	)
	if err := row.Scan(&serviceID, &record.AllocationID, &record.DesiredSpecDigest, &record.EnvironmentID, &record.NodeID, &record.NodeTarget, &record.Attempt, &statusText, &record.Ready, &record.ReadinessMessage, &readinessProbeJSON, &livenessProbeJSON, &configJSON); err != nil {
		return "", nil, err
	}
	if err := hydrateAllocationRecord(&record, statusText, readinessProbeJSON, livenessProbeJSON, configJSON); err != nil {
		return "", nil, err
	}
	record.ServiceID = serviceID
	return serviceID, &record, nil
}

func hydrateAllocationRecord(record *servicekernel.AllocationRecord, statusText string, readinessProbeJSON, livenessProbeJSON, configJSON []byte) error {
	record.Status = commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
	if n, ok := commonv1.AllocationStatus_value[statusText]; ok {
		record.Status = commonv1.AllocationStatus(n)
	}
	if len(readinessProbeJSON) > 0 && string(readinessProbeJSON) != "null" {
		record.ReadinessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(readinessProbeJSON, record.ReadinessProbe); err != nil {
			return fmt.Errorf("unmarshal service allocation readiness probe: %w", err)
		}
		record.ReadinessProbe = servicekernel.NormalizeReadinessProbe(record.ReadinessProbe)
	}
	if len(livenessProbeJSON) > 0 && string(livenessProbeJSON) != "null" {
		record.LivenessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(livenessProbeJSON, record.LivenessProbe); err != nil {
			return fmt.Errorf("unmarshal service allocation liveness probe: %w", err)
		}
		record.LivenessProbe = servicekernel.NormalizeLivenessProbe(record.LivenessProbe)
	}
	record.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, record.Config); err != nil {
		return fmt.Errorf("unmarshal service allocation config: %w", err)
	}
	return nil
}
