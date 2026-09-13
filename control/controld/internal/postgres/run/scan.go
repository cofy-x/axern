package pgrun

import (
	"fmt"
	"strings"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func environmentSelectSQL() string {
	return `SELECT environment_id, namespace, status, spec_hash, spec, resolved_template,
		labels, version, created_at, updated_at, message FROM environments`
}

func runSelectSQL() string {
	return `SELECT r.run_id, r.namespace, r.environment_id, a.allocation_id, r.status,
		r.config, r.labels, r.version, r.created_at, r.updated_at, r.exit_code, r.exit_code_known, r.diagnostic_code, r.message,
		a.node_id,
		a.workspace_preparation,
		COALESCE((SELECT revision FROM allocation_capability_condition_sets s WHERE s.allocation_id = a.allocation_id), 0),
		(SELECT observed_at FROM allocation_capability_condition_sets s WHERE s.allocation_id = a.allocation_id),
		COALESCE((
			SELECT jsonb_build_object('conditions', COALESCE(jsonb_agg(c.condition ORDER BY c.capability_key_id), '[]'::jsonb))
			FROM allocation_capability_conditions c WHERE c.allocation_id = a.allocation_id
		), '{"conditions":[]}'::jsonb)
		FROM runs r JOIN allocations a ON a.run_id = r.run_id`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEnvironment(row scanner) (*environmentv1.Environment, error) {
	var (
		env                    environmentv1.Environment
		statusText             string
		specJSON, templateJSON []byte
		labelsJSON             []byte
		createdAt, updatedAt   time.Time
	)
	if err := row.Scan(&env.ID, &env.Namespace, &statusText, &env.SpecHash, &specJSON, &templateJSON, &labelsJSON, &env.Version, &createdAt, &updatedAt, &env.Message); err != nil {
		return nil, err
	}
	env.Status = environmentkernel.ParseStatus(statusText)
	env.Spec = &environmentv1.EnvironmentSpec{}
	if err := protojson.Unmarshal(specJSON, env.Spec); err != nil {
		return nil, fmt.Errorf("unmarshal environment spec: %w", err)
	}
	env.ResolvedTemplate = &catalogv1.RuntimeTemplate{}
	if err := protojson.Unmarshal(templateJSON, env.ResolvedTemplate); err != nil {
		return nil, fmt.Errorf("unmarshal resolved template: %w", err)
	}
	env.Labels = unmarshalJSONMap(labelsJSON)
	env.CreatedAt = timestamppb.New(createdAt)
	env.UpdatedAt = timestamppb.New(updatedAt)
	return &env, nil
}

func scanRun(row scanner) (*runv1.Run, error) {
	var (
		run                                                                        runv1.Run
		statusText                                                                 string
		diagnosticCodeText                                                         string
		configJSON, labelsJSON, workspacePreparationJSON, capabilityConditionsJSON []byte
		createdAt, updatedAt                                                       time.Time
		capabilityRevision                                                         int64
		capabilityObservedAt                                                       pgtype.Timestamptz
	)
	if err := row.Scan(&run.ID, &run.Namespace, &run.EnvironmentID, &run.AllocationID, &statusText, &configJSON, &labelsJSON, &run.Version, &createdAt, &updatedAt, &run.ExitCode, &run.ExitCodeKnown, &diagnosticCodeText, &run.Message, &run.NodeID, &workspacePreparationJSON, &capabilityRevision, &capabilityObservedAt, &capabilityConditionsJSON); err != nil {
		return nil, err
	}
	run.Status = parseRunStatus(statusText)
	run.DiagnosticCode = parseWorkloadDiagnosticCode(diagnosticCodeText)
	run.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, run.Config); err != nil {
		return nil, fmt.Errorf("unmarshal run config: %w", err)
	}
	run.Labels = unmarshalJSONMap(labelsJSON)
	if string(workspacePreparationJSON) != "null" {
		run.WorkspacePreparation = &commonv1.WorkspacePreparationFacts{}
		if err := protojson.Unmarshal(workspacePreparationJSON, run.WorkspacePreparation); err != nil {
			return nil, fmt.Errorf("unmarshal run workspace preparation: %w", err)
		}
	}
	conditionSet := &capabilityv1.CapabilityConditionSet{}
	if err := protojson.Unmarshal(capabilityConditionsJSON, conditionSet); err != nil {
		return nil, fmt.Errorf("unmarshal run capability conditions: %w", err)
	}
	if capabilityRevision > 0 && capabilityObservedAt.Valid {
		conditionSet.Revision = capabilityRevision
		conditionSet.ObservedAt = timestamppb.New(capabilityObservedAt.Time)
		run.CapabilityConditions = conditionSet
	}
	run.CreatedAt = timestamppb.New(createdAt)
	run.UpdatedAt = timestamppb.New(updatedAt)
	return &run, nil
}

func parseWorkloadDiagnosticCode(value string) commonv1.WorkloadDiagnosticCode {
	if number, ok := commonv1.WorkloadDiagnosticCode_value[strings.TrimSpace(value)]; ok {
		return commonv1.WorkloadDiagnosticCode(number)
	}
	return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
}

func scanLease(row scanner) (*commonv1.ExecutionLease, error) {
	var (
		lease     commonv1.ExecutionLease
		leaseType string
		expiresAt time.Time
		tokenHash string
	)
	if err := row.Scan(&lease.LeaseID, &lease.AllocationID, &lease.NodeID, &lease.NodeTarget, &leaseType, &expiresAt, &lease.Revision, &lease.Revoked, &tokenHash); err != nil {
		return nil, err
	}
	lease.LeaseType = leasekernel.ParseType(leaseType)
	lease.ExpiresAt = timestamppb.New(expiresAt)
	lease.ValidationTokenHash = tokenHash
	return &lease, nil
}
