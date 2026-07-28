package pgfunction

import (
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type scanner interface {
	Scan(dest ...any) error
}

func functionSelectSQL() string {
	return `SELECT function_id, namespace, name, active_revision_id, spec, status, deployment_status, labels, version, created_at, updated_at, message, diagnostic_code FROM functions`
}

func revisionSelectSQL() string {
	return `SELECT revision_id, function_id, namespace, name, revision_number, spec, source, source_digest, manifest_digest, labels, created_at, created_by FROM function_revisions`
}

func deploymentSelectSQL() string {
	return `SELECT function_id, active_revision_id, status, scaling, desired_replicas, ready_replicas, active_invocations, updated_at, message, diagnostic_code, worker_service_id FROM function_deployments`
}

func invocationSelectSQL() string {
	return `SELECT invocation_id, function_id, function_name, namespace, revision_id, status, mode, payload, result, error, timeout, duration, request_id, labels, created_at, started_at, completed_at, message, diagnostic_code FROM function_invocations`
}

func scanFunction(row scanner) (*functionv1.Function, error) {
	var (
		fn                     functionv1.Function
		specJSON, labelsJSON   []byte
		statusText, deployText string
		diagnosticText         string
		createdAt, updatedAt   time.Time
	)
	if err := row.Scan(&fn.ID, &fn.Namespace, &fn.Name, &fn.ActiveRevisionID, &specJSON, &statusText, &deployText, &labelsJSON, &fn.Version, &createdAt, &updatedAt, &fn.Message, &diagnosticText); err != nil {
		return nil, err
	}
	fn.Spec = &functionv1.FunctionSpec{}
	if err := protojson.Unmarshal(specJSON, fn.Spec); err != nil {
		return nil, fmt.Errorf("unmarshal function spec: %w", err)
	}
	fn.Status = parseFunctionStatus(statusText)
	fn.DeploymentStatus = parseDeploymentStatus(deployText)
	fn.Labels = unmarshalJSONMap(labelsJSON)
	fn.CreatedAt = timestamppb.New(createdAt)
	fn.UpdatedAt = timestamppb.New(updatedAt)
	fn.DiagnosticCode = parseDiagnosticCode(diagnosticText)
	return &fn, nil
}

func scanRevision(row scanner) (*functionv1.FunctionRevision, error) {
	var (
		revision             functionv1.FunctionRevision
		specJSON, sourceJSON []byte
		labelsJSON           []byte
		createdAt            time.Time
	)
	if err := row.Scan(&revision.ID, &revision.FunctionID, &revision.Namespace, &revision.Name, &revision.RevisionNumber, &specJSON, &sourceJSON, &revision.SourceDigest, &revision.ManifestDigest, &labelsJSON, &createdAt, &revision.CreatedBy); err != nil {
		return nil, err
	}
	revision.Spec = &functionv1.FunctionSpec{}
	if err := protojson.Unmarshal(specJSON, revision.Spec); err != nil {
		return nil, fmt.Errorf("unmarshal function revision spec: %w", err)
	}
	revision.Source = &functionv1.FunctionSource{}
	if err := protojson.Unmarshal(sourceJSON, revision.Source); err != nil {
		return nil, fmt.Errorf("unmarshal function revision source: %w", err)
	}
	revision.Labels = unmarshalJSONMap(labelsJSON)
	revision.CreatedAt = timestamppb.New(createdAt)
	return &revision, nil
}

func scanDeployment(row scanner) (*functionv1.FunctionDeployment, error) {
	var (
		deployment     functionv1.FunctionDeployment
		statusText     string
		scalingJSON    []byte
		updatedAt      time.Time
		diagnosticText string
	)
	if err := row.Scan(&deployment.FunctionID, &deployment.ActiveRevisionID, &statusText, &scalingJSON, &deployment.DesiredReplicas, &deployment.ReadyReplicas, &deployment.ActiveInvocations, &updatedAt, &deployment.Message, &diagnosticText, &deployment.WorkerServiceID); err != nil {
		return nil, err
	}
	deployment.Status = parseDeploymentStatus(statusText)
	deployment.Scaling = &functionv1.FunctionScalingSpec{}
	if err := protojson.Unmarshal(scalingJSON, deployment.Scaling); err != nil {
		return nil, fmt.Errorf("unmarshal function deployment scaling: %w", err)
	}
	deployment.UpdatedAt = timestamppb.New(updatedAt)
	deployment.DiagnosticCode = parseDiagnosticCode(diagnosticText)
	return &deployment, nil
}

func scanInvocation(row scanner) (*functionv1.FunctionInvocation, error) {
	var (
		invocation                                                    functionv1.FunctionInvocation
		statusText, modeText, diagnosticText                          string
		payloadJSON, resultJSON, errorJSON, timeoutJSON, durationJSON []byte
		labelsJSON                                                    []byte
		createdAt                                                     time.Time
		startedAt, completedAt                                        pgtype.Timestamptz
	)
	if err := row.Scan(&invocation.ID, &invocation.FunctionID, &invocation.FunctionName, &invocation.Namespace, &invocation.RevisionID, &statusText, &modeText, &payloadJSON, &resultJSON, &errorJSON, &timeoutJSON, &durationJSON, &invocation.RequestID, &labelsJSON, &createdAt, &startedAt, &completedAt, &invocation.Message, &diagnosticText); err != nil {
		return nil, err
	}
	invocation.Status = parseInvocationStatus(statusText)
	invocation.Mode = parseInvocationMode(modeText)
	invocation.Payload = &functionv1.FunctionPayload{}
	if err := protojson.Unmarshal(payloadJSON, invocation.Payload); err != nil {
		return nil, fmt.Errorf("unmarshal function invocation payload: %w", err)
	}
	invocation.Result = &functionv1.FunctionResult{}
	if err := protojson.Unmarshal(resultJSON, invocation.Result); err != nil {
		return nil, fmt.Errorf("unmarshal function invocation result: %w", err)
	}
	invocation.Error = &functionv1.FunctionError{}
	if err := protojson.Unmarshal(errorJSON, invocation.Error); err != nil {
		return nil, fmt.Errorf("unmarshal function invocation error: %w", err)
	}
	invocation.Timeout = &durationpb.Duration{}
	if err := unmarshalDurationJSON(timeoutJSON, invocation.Timeout); err != nil {
		return nil, fmt.Errorf("unmarshal function invocation timeout: %w", err)
	}
	invocation.Duration = &durationpb.Duration{}
	if err := unmarshalDurationJSON(durationJSON, invocation.Duration); err != nil {
		return nil, fmt.Errorf("unmarshal function invocation duration: %w", err)
	}
	invocation.Labels = unmarshalJSONMap(labelsJSON)
	invocation.CreatedAt = timestamppb.New(createdAt)
	if startedAt.Valid {
		invocation.StartedAt = timestamppb.New(startedAt.Time)
	}
	if completedAt.Valid {
		invocation.CompletedAt = timestamppb.New(completedAt.Time)
	}
	invocation.DiagnosticCode = parseDiagnosticCode(diagnosticText)
	return &invocation, nil
}

func unmarshalDurationJSON(payload []byte, target *durationpb.Duration) error {
	if len(payload) == 0 || strings.TrimSpace(string(payload)) == "{}" || strings.TrimSpace(string(payload)) == "null" {
		return nil
	}
	return protojson.Unmarshal(payload, target)
}

func scanEvent(row scanner) (*functionv1.FunctionEvent, error) {
	var (
		event       functionv1.FunctionEvent
		typeText    string
		detailsJSON []byte
		createdAt   time.Time
	)
	if err := row.Scan(&event.ID, &event.FunctionID, &event.InvocationID, &event.RevisionID, &typeText, &event.Message, &detailsJSON, &createdAt); err != nil {
		return nil, err
	}
	event.Type = parseEventType(typeText)
	event.Details = unmarshalJSONMap(detailsJSON)
	event.CreatedAt = timestamppb.New(createdAt)
	return &event, nil
}

func parseFunctionStatus(value string) functionv1.FunctionStatus {
	if n, ok := functionv1.FunctionStatus_value[value]; ok {
		return functionv1.FunctionStatus(n)
	}
	return functionv1.FunctionStatus_FUNCTION_STATUS_UNSPECIFIED
}

func parseDeploymentStatus(value string) functionv1.FunctionDeploymentStatus {
	if n, ok := functionv1.FunctionDeploymentStatus_value[value]; ok {
		return functionv1.FunctionDeploymentStatus(n)
	}
	return functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_UNSPECIFIED
}

func parseInvocationStatus(value string) functionv1.FunctionInvocationStatus {
	if n, ok := functionv1.FunctionInvocationStatus_value[value]; ok {
		return functionv1.FunctionInvocationStatus(n)
	}
	return functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_UNSPECIFIED
}

func parseInvocationMode(value string) functionv1.FunctionInvocationMode {
	if n, ok := functionv1.FunctionInvocationMode_value[value]; ok {
		return functionv1.FunctionInvocationMode(n)
	}
	return functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_UNSPECIFIED
}

func parseEventType(value string) functionv1.FunctionEventType {
	if n, ok := functionv1.FunctionEventType_value[value]; ok {
		return functionv1.FunctionEventType(n)
	}
	return functionv1.FunctionEventType_FUNCTION_EVENT_TYPE_UNSPECIFIED
}

func parseDiagnosticCode(value string) commonv1.WorkloadDiagnosticCode {
	if n, ok := commonv1.WorkloadDiagnosticCode_value[value]; ok {
		return commonv1.WorkloadDiagnosticCode(n)
	}
	return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
}
