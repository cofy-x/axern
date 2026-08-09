package pgrun

import (
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
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
	return `SELECT run_id, namespace, environment_id, allocation_id, attempt, status,
		config, labels, version, created_at, updated_at, exit_code, exit_code_known, message,
		COALESCE((SELECT revision FROM allocation_capability_condition_sets s WHERE s.allocation_id = runs.allocation_id AND s.allocation_attempt = runs.attempt), 0),
		(SELECT observed_at FROM allocation_capability_condition_sets s WHERE s.allocation_id = runs.allocation_id AND s.allocation_attempt = runs.attempt),
		COALESCE((
			SELECT jsonb_build_object('conditions', COALESCE(jsonb_agg(c.condition ORDER BY c.capability_key_id), '[]'::jsonb))
			FROM allocation_capability_conditions c WHERE c.allocation_id = runs.allocation_id AND c.allocation_attempt = runs.attempt
		), '{"conditions":[]}'::jsonb)
		FROM runs`
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
		run                                              runv1.Run
		statusText                                       string
		configJSON, labelsJSON, capabilityConditionsJSON []byte
		createdAt, updatedAt                             time.Time
		capabilityRevision                               int64
		capabilityObservedAt                             pgtype.Timestamptz
	)
	if err := row.Scan(&run.ID, &run.Namespace, &run.EnvironmentID, &run.AllocationID, &run.Attempt, &statusText, &configJSON, &labelsJSON, &run.Version, &createdAt, &updatedAt, &run.ExitCode, &run.ExitCodeKnown, &run.Message, &capabilityRevision, &capabilityObservedAt, &capabilityConditionsJSON); err != nil {
		return nil, err
	}
	run.Status = parseRunStatus(statusText)
	run.DiagnosticCode = workloadkernel.ClassifyDiagnostic(runDiagnosticAllocationStatus(run.GetStatus(), run.GetExitCodeKnown()), run.GetMessage())
	run.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, run.Config); err != nil {
		return nil, fmt.Errorf("unmarshal run config: %w", err)
	}
	run.Labels = unmarshalJSONMap(labelsJSON)
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

func runDiagnosticAllocationStatus(status runv1.RunStatus, exitCodeKnown bool) commonv1.AllocationStatus {
	switch status {
	case runv1.RunStatus_RUN_STATUS_FAILED:
		if exitCodeKnown {
			return commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED
		}
		return commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	default:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
	}
}

func scanLease(row scanner) (*commonv1.ExecutionLease, error) {
	var (
		lease     commonv1.ExecutionLease
		leaseType string
		expiresAt time.Time
		tokenHash string
	)
	if err := row.Scan(&lease.LeaseID, &lease.AllocationID, &lease.NodeID, &lease.NodeTarget, &lease.Attempt, &leaseType, &expiresAt, &lease.Revision, &lease.Revoked, &tokenHash); err != nil {
		return nil, err
	}
	lease.LeaseType = leasekernel.ParseType(leaseType)
	lease.ExpiresAt = timestamppb.New(expiresAt)
	lease.ValidationTokenHash = tokenHash
	return &lease, nil
}
