package functionkernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultFunctionEventLimit int32 = 50
const defaultFunctionBundleMediaType = "application/vnd.axern.function.tar"
const FunctionBundleStorageURIPrefix = "axern://function-bundles/"

func NormalizeNamespace(namespace string) string {
	return environmentkernel.NormalizeNamespace(namespace)
}

func NormalizeName(name string) string {
	return strings.TrimSpace(name)
}

func NewFunction(params DeployParams, revisionID string, now time.Time) *functionv1.Function {
	return &functionv1.Function{
		ID:               "fn-" + uuid.NewString(),
		Namespace:        NormalizeNamespace(params.Namespace),
		Name:             NormalizeName(params.Name),
		ActiveRevisionID: strings.TrimSpace(revisionID),
		Spec:             CloneSpec(params.Spec),
		Status:           functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING,
		DeploymentStatus: functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_PENDING,
		Labels:           cloneLabels(params.Labels),
		Version:          1,
		CreatedAt:        timestamppb.New(now.UTC()),
		UpdatedAt:        timestamppb.New(now.UTC()),
		Message:          "function deploy persisted; worker rollout pending",
		DiagnosticCode:   commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
	}
}

func NewRevision(functionID string, revisionNumber int64, params DeployParams, sourceDigest, manifestDigest string, now time.Time) *functionv1.FunctionRevision {
	return &functionv1.FunctionRevision{
		ID:             "fnrev-" + uuid.NewString(),
		FunctionID:     strings.TrimSpace(functionID),
		Namespace:      NormalizeNamespace(params.Namespace),
		Name:           NormalizeName(params.Name),
		RevisionNumber: revisionNumber,
		Spec:           CloneSpec(params.Spec),
		Source:         CloneSource(params.Source),
		SourceDigest:   strings.TrimSpace(sourceDigest),
		ManifestDigest: strings.TrimSpace(manifestDigest),
		Labels:         cloneLabels(params.Labels),
		CreatedAt:      timestamppb.New(now.UTC()),
	}
}

func NewDeployment(functionID, revisionID string, scaling *functionv1.FunctionScalingSpec, now time.Time) *functionv1.FunctionDeployment {
	return &functionv1.FunctionDeployment{
		FunctionID:        strings.TrimSpace(functionID),
		ActiveRevisionID:  strings.TrimSpace(revisionID),
		Status:            functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_PENDING,
		Scaling:           CloneScaling(scaling),
		DesiredReplicas:   0,
		ReadyReplicas:     0,
		ActiveInvocations: 0,
		UpdatedAt:         timestamppb.New(now.UTC()),
		Message:           "function worker rollout pending",
		DiagnosticCode:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
	}
}

func MarkWorkerServiceAttached(current *functionv1.Function, deployment *functionv1.FunctionDeployment, serviceID string, desiredReplicas int32, now time.Time) (*functionv1.Function, *functionv1.FunctionDeployment) {
	if current == nil || deployment == nil {
		return nil, nil
	}
	nextFunction := CloneFunction(current)
	nextFunction.Status = functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING
	nextFunction.DeploymentStatus = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
	nextFunction.Version++
	nextFunction.UpdatedAt = timestamppb.New(now.UTC())
	nextFunction.Message = "function worker rollout is warming"
	nextFunction.DiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED

	nextDeployment := CloneDeployment(deployment)
	nextDeployment.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
	nextDeployment.DesiredReplicas = desiredReplicas
	nextDeployment.ReadyReplicas = 0
	nextDeployment.UpdatedAt = timestamppb.New(now.UTC())
	nextDeployment.Message = "function worker service created"
	nextDeployment.WorkerServiceID = strings.TrimSpace(serviceID)
	nextDeployment.DiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
	if desiredReplicas == 0 {
		nextFunction.Status = functionv1.FunctionStatus_FUNCTION_STATUS_READY
		nextFunction.DeploymentStatus = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
		nextFunction.Message = "function worker service is scaled to zero"
		nextDeployment.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
		nextDeployment.Message = "function worker service is scaled to zero"
	}
	return nextFunction, nextDeployment
}

func NewBundleSource(params UploadBundleParams) *functionv1.FunctionBundleSource {
	digest := strings.TrimSpace(params.Digest)
	mediaType := strings.TrimSpace(params.MediaType)
	if mediaType == "" {
		mediaType = defaultFunctionBundleMediaType
	}
	return &functionv1.FunctionBundleSource{
		Digest:     digest,
		MediaType:  mediaType,
		SizeBytes:  params.SizeBytes,
		StorageUri: BundleStorageURI(digest),
	}
}

func BundleStorageURI(digest string) string {
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(digest, "sha256:") {
		return fmt.Sprintf("%s%s.tar", FunctionBundleStorageURIPrefix, strings.TrimPrefix(digest, "sha256:"))
	}
	return FunctionBundleStorageURIPrefix + digest
}

func MarkDeleted(current *functionv1.Function, now time.Time) *functionv1.Function {
	if current == nil {
		return nil
	}
	next := CloneFunction(current)
	next.Status = functionv1.FunctionStatus_FUNCTION_STATUS_DELETED
	next.DeploymentStatus = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
	next.Version++
	next.UpdatedAt = timestamppb.New(now.UTC())
	next.Message = "function deleted; worker service cleanup requested"
	next.DiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
	return next
}

func MarkDeploymentDeleted(current *functionv1.FunctionDeployment, functionID, revisionID string, scaling *functionv1.FunctionScalingSpec, now time.Time) *functionv1.FunctionDeployment {
	var next *functionv1.FunctionDeployment
	if current == nil {
		next = NewDeployment(functionID, revisionID, scaling, now)
	} else {
		next = CloneDeployment(current)
	}
	next.FunctionID = strings.TrimSpace(functionID)
	next.ActiveRevisionID = strings.TrimSpace(revisionID)
	next.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
	next.Scaling = CloneScaling(scaling)
	next.DesiredReplicas = 0
	next.ReadyReplicas = 0
	next.ActiveInvocations = 0
	next.UpdatedAt = timestamppb.New(now.UTC())
	next.Message = "function worker service cleanup requested"
	next.DiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
	return next
}

func NewEvent(functionID, invocationID, revisionID string, eventType functionv1.FunctionEventType, message string, details map[string]string, now time.Time) *functionv1.FunctionEvent {
	return &functionv1.FunctionEvent{
		ID:           "fnevt-" + uuid.NewString(),
		FunctionID:   strings.TrimSpace(functionID),
		InvocationID: strings.TrimSpace(invocationID),
		RevisionID:   strings.TrimSpace(revisionID),
		Type:         eventType,
		Message:      strings.TrimSpace(message),
		Details:      cloneLabels(details),
		CreatedAt:    timestamppb.New(now.UTC()),
	}
}

func NewInvocation(fn *functionv1.Function, revision *functionv1.FunctionRevision, params InvokeParams, now time.Time) *functionv1.FunctionInvocation {
	status := functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_RUNNING
	if params.Mode == functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
		status = functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_QUEUED
	}

	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" {
		requestID = "auto-" + uuid.NewString()
	}

	invocation := &functionv1.FunctionInvocation{
		ID:           "fninv-" + uuid.NewString(),
		FunctionID:   strings.TrimSpace(fn.GetID()),
		FunctionName: strings.TrimSpace(fn.GetName()),
		Namespace:    NormalizeNamespace(fn.GetNamespace()),
		RevisionID:   strings.TrimSpace(revision.GetID()),
		Status:       status,
		Mode:         params.Mode,
		Payload:      ClonePayload(params.Payload),
		Timeout:      cloneDuration(params.Timeout),
		RequestID:    requestID,
		Labels:       cloneLabels(params.Labels),
		CreatedAt:    timestamppb.New(now.UTC()),
	}
	if params.Mode != functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
		invocation.StartedAt = timestamppb.New(now.UTC())
	}
	return invocation
}

func FinishInvocation(current *functionv1.FunctionInvocation, status functionv1.FunctionInvocationStatus, result *functionv1.FunctionResult, fnErr *functionv1.FunctionError, message string, now time.Time) *functionv1.FunctionInvocation {
	if current == nil {
		return nil
	}
	next := CloneInvocation(current)
	next.Status = status
	next.Result = CloneResult(result)
	next.Error = CloneError(fnErr)
	next.CompletedAt = timestamppb.New(now.UTC())
	next.Message = strings.TrimSpace(message)
	if next.GetStartedAt() != nil {
		next.Duration = durationpb.New(now.UTC().Sub(next.GetStartedAt().AsTime()))
	}
	return next
}

func ManifestDigest(spec *functionv1.FunctionSpec) string {
	return digestProto(CloneSpec(spec))
}

func SourceDigest(source *functionv1.FunctionSource) string {
	switch src := source.GetSource().(type) {
	case *functionv1.FunctionSource_Bundle:
		if digest := strings.TrimSpace(src.Bundle.GetDigest()); digest != "" {
			return digest
		}
	case *functionv1.FunctionSource_Image:
		if digest := strings.TrimSpace(src.Image.GetDigest()); digest != "" {
			return digest
		}
	}
	return digestProto(CloneSource(source))
}

func NormalizeFunctionEventLimit(limit int32) int32 {
	if limit <= 0 || limit > defaultFunctionEventLimit {
		return defaultFunctionEventLimit
	}
	return limit
}

func MatchFunctionFilter(fn *functionv1.Function, filter *functionv1.FunctionListFilter) bool {
	if fn == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" && fn.GetNamespace() != NormalizeNamespace(namespace) {
		return false
	}
	if len(filter.GetStatuses()) > 0 {
		matched := false
		for _, status := range filter.GetStatuses() {
			if fn.GetStatus() == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return labelsMatch(fn.GetLabels(), filter.GetLabels())
}

func MatchInvocationFilter(invocation *functionv1.FunctionInvocation, filter *functionv1.FunctionInvocationListFilter) bool {
	if invocation == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" && invocation.GetNamespace() != NormalizeNamespace(namespace) {
		return false
	}
	if functionID := strings.TrimSpace(filter.GetFunctionID()); functionID != "" && invocation.GetFunctionID() != functionID {
		return false
	}
	if functionName := strings.TrimSpace(filter.GetFunctionName()); functionName != "" && invocation.GetFunctionName() != functionName {
		return false
	}
	if revisionID := strings.TrimSpace(filter.GetRevisionID()); revisionID != "" && invocation.GetRevisionID() != revisionID {
		return false
	}
	if len(filter.GetStatuses()) > 0 {
		matched := false
		for _, status := range filter.GetStatuses() {
			if invocation.GetStatus() == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return labelsMatch(invocation.GetLabels(), filter.GetLabels())
}

func labelsMatch(have, want map[string]string) bool {
	for key, value := range want {
		if have[strings.TrimSpace(key)] != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func CloneLabels(in map[string]string) map[string]string {
	return cloneLabels(in)
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func digestProto(message proto.Message) string {
	if message == nil {
		return ""
	}
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
