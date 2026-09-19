package pgrun

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func environmentSelectSQL() string {
	return `SELECT environment_id, namespace, spec, resolved_spec, labels, created_at FROM environments`
}

func runSelectSQL() string {
	return `SELECT r.run_id, r.namespace, r.environment_id, a.allocation_id, r.status,
		r.config, r.environment_spec, r.resolved_environment_spec, r.labels, r.version, r.created_at, r.updated_at, r.exit_code, r.diagnostic_code, r.message, r.rootfs_snapshot_result,
		a.node_id, a.output_expires_at,
		COALESCE((SELECT conditions FROM allocation_capability_conditions c WHERE c.allocation_id = a.allocation_id), '{}'::jsonb)
		FROM runs r JOIN allocations a ON a.run_id = r.run_id`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEnvironment(row scanner) (*environmentv1.Environment, error) {
	var (
		env                        environmentv1.Environment
		specJSON, resolvedSpecJSON []byte
		labelsJSON                 []byte
		createdAt                  time.Time
	)
	if err := row.Scan(&env.ID, &env.Namespace, &specJSON, &resolvedSpecJSON, &labelsJSON, &createdAt); err != nil {
		return nil, err
	}
	env.Spec = &environmentv1.EnvironmentSpec{}
	if err := protojson.Unmarshal(specJSON, env.Spec); err != nil {
		return nil, fmt.Errorf("unmarshal environment spec: %w", err)
	}
	env.ResolvedSpec = &environmentv1.ResolvedEnvironmentSpec{}
	if err := protojson.Unmarshal(resolvedSpecJSON, env.ResolvedSpec); err != nil {
		return nil, fmt.Errorf("unmarshal resolved environment spec: %w", err)
	}
	env.Labels = unmarshalJSONMap(labelsJSON)
	env.CreatedAt = timestamppb.New(createdAt)
	return &env, nil
}

func scanRun(row scanner) (*runv1.Run, error) {
	var (
		run                         runv1.Run
		statusText                  string
		diagnosticCodeText          string
		configJSON                  []byte
		environmentSpecJSON         []byte
		resolvedEnvironmentSpecJSON []byte
		labelsJSON                  []byte
		capabilityConditionsJSON    []byte
		rootfsSnapshotJSON          []byte
		createdAt, updatedAt        time.Time
		exitCode                    sql.NullInt32
		outputExpiry                sql.NullTime
		ignoredNodeID               string
	)
	if err := row.Scan(&run.ID, &run.Namespace, &run.EnvironmentID, &run.AllocationID, &statusText, &configJSON, &environmentSpecJSON, &resolvedEnvironmentSpecJSON, &labelsJSON, &run.Version, &createdAt, &updatedAt, &exitCode, &diagnosticCodeText, &run.Message, &rootfsSnapshotJSON, &ignoredNodeID, &outputExpiry, &capabilityConditionsJSON); err != nil {
		return nil, err
	}
	run.Status = parseRunStatus(statusText)
	if outputExpiry.Valid {
		run.OutputExpiresAt = timestamppb.New(outputExpiry.Time)
	}
	if exitCode.Valid {
		value := exitCode.Int32
		run.ExitCode = &value
	}
	run.DiagnosticCode = parseWorkloadDiagnosticCode(diagnosticCodeText)
	run.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, run.Config); err != nil {
		return nil, fmt.Errorf("unmarshal run config: %w", err)
	}
	run.EnvironmentSpec = &environmentv1.EnvironmentSpec{}
	if err := protojson.Unmarshal(environmentSpecJSON, run.EnvironmentSpec); err != nil {
		return nil, fmt.Errorf("unmarshal run environment spec: %w", err)
	}
	run.ResolvedEnvironmentSpec = &environmentv1.ResolvedEnvironmentSpec{}
	if err := protojson.Unmarshal(resolvedEnvironmentSpecJSON, run.ResolvedEnvironmentSpec); err != nil {
		return nil, fmt.Errorf("unmarshal run resolved environment spec: %w", err)
	}
	run.Labels = unmarshalJSONMap(labelsJSON)
	rootfsSnapshot := &runv1.RootfsSnapshotResult{}
	if err := protojson.Unmarshal(rootfsSnapshotJSON, rootfsSnapshot); err != nil {
		return nil, fmt.Errorf("unmarshal rootfs snapshot result: %w", err)
	}
	if rootfsSnapshot.GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_UNSPECIFIED {
		run.RootfsSnapshot = rootfsSnapshot
	}
	conditionSet := &capabilityv1.CapabilityConditionSet{}
	if err := protojson.Unmarshal(capabilityConditionsJSON, conditionSet); err != nil {
		return nil, fmt.Errorf("unmarshal run capability conditions: %w", err)
	}
	if conditionSet.GetObservedAt() != nil {
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
