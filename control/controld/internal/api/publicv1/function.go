package publicv1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const maxFunctionBundleBytes int64 = 64 << 20

func (s *Server) UploadFunctionBundle(stream functionv1.FunctionControl_UploadFunctionBundleServer) error {
	if s.deps.Functions == nil {
		return grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	_, op := publicOps.Function(stream.Context(), "FunctionControl.UploadFunctionBundle", publicActionUpload)
	var err error
	defer func() { op.End(err) }()

	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = grpcstatus.Error(codes.InvalidArgument, "upload open is required")
			return err
		}
		return err
	}
	open := first.GetOpen()
	if open == nil {
		err = grpcstatus.Error(codes.InvalidArgument, "upload open is required before chunks")
		return err
	}
	if err = validateUploadFunctionBundleOpen(open); err != nil {
		return err
	}

	var payload bytes.Buffer
	payload.Grow(int(open.GetSizeBytes()))
	for {
		req, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			err = recvErr
			return err
		}
		chunk := req.GetChunk()
		if chunk == nil {
			err = grpcstatus.Error(codes.InvalidArgument, "upload chunk is required after open")
			return err
		}
		if int64(payload.Len()+len(chunk)) > maxFunctionBundleBytes {
			err = grpcstatus.Errorf(codes.InvalidArgument, "function bundle exceeds %d bytes", maxFunctionBundleBytes)
			return err
		}
		if _, err = payload.Write(chunk); err != nil {
			return err
		}
	}
	if int64(payload.Len()) != open.GetSizeBytes() {
		err = grpcstatus.Errorf(codes.InvalidArgument, "function bundle size = %d, want %d", payload.Len(), open.GetSizeBytes())
		return err
	}
	sum := sha256.Sum256(payload.Bytes())
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != strings.TrimSpace(open.GetDigest()) {
		err = grpcstatus.Errorf(codes.InvalidArgument, "function bundle digest = %q, want %q", got, strings.TrimSpace(open.GetDigest()))
		return err
	}
	resp, err := s.deps.Functions.UploadFunctionBundle(stream.Context(), functionkernel.UploadBundleParams{
		Namespace: open.GetNamespace(),
		Name:      open.GetName(),
		Digest:    open.GetDigest(),
		MediaType: open.GetMediaType(),
		SizeBytes: open.GetSizeBytes(),
		Payload:   payload.Bytes(),
	}, s.deps.Now())
	if err != nil {
		err = functionError(err, "function bundle upload is not implemented")
		return err
	}
	return stream.SendAndClose(resp)
}

func (s *Server) DeployFunction(ctx context.Context, req *functionv1.DeployFunctionRequest) (*functionv1.DeployFunctionResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.DeployFunction", publicActionDeploy, withNamespace(req.GetNamespace()))
	var err error
	defer func() { op.End(err) }()

	if err = validateDeployFunctionRequest(req); err != nil {
		return nil, err
	}
	resp, err := s.deps.Functions.DeployFunction(ctx, req, s.deps.Now())
	if err != nil {
		err = functionError(err, "function deploy is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) GetFunction(ctx context.Context, req *functionv1.GetFunctionRequest) (*functionv1.GetFunctionResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.GetFunction", publicActionGet, withFunctionID(req.GetFunctionID()))
	var err error
	defer func() { op.End(err) }()

	if err = validateFunctionLookup(req.GetFunctionID(), req.GetNamespace(), req.GetName()); err != nil {
		return nil, err
	}
	resp, err := s.deps.Functions.GetFunction(ctx, req)
	if err != nil {
		err = functionError(err, "function get is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) ListFunctions(ctx context.Context, req *functionv1.ListFunctionsRequest) (*functionv1.ListFunctionsResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.ListFunctions", publicActionList)
	var err error
	defer func() { op.End(err) }()

	resp, err := s.deps.Functions.ListFunctions(ctx, req)
	if err != nil {
		err = functionError(err, "function list is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) DeleteFunction(ctx context.Context, req *functionv1.DeleteFunctionRequest) (*functionv1.DeleteFunctionResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.DeleteFunction", publicActionDelete, withFunctionID(req.GetFunctionID()))
	var err error
	defer func() { op.End(err) }()

	if err = validateFunctionLookup(req.GetFunctionID(), req.GetNamespace(), req.GetName()); err != nil {
		return nil, err
	}
	resp, err := s.deps.Functions.DeleteFunction(ctx, req, s.deps.Now())
	if err != nil {
		err = functionError(err, "function delete is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) InvokeFunction(ctx context.Context, req *functionv1.InvokeFunctionRequest) (*functionv1.InvokeFunctionResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.InvokeFunction", publicActionInvoke, withFunctionID(req.GetFunctionID()))
	var err error
	defer func() { op.End(err) }()

	if err = validateFunctionInvokeRequest(req); err != nil {
		return nil, err
	}
	resp, err := s.deps.Functions.InvokeFunction(ctx, req, s.deps.Now())
	if err != nil {
		err = functionError(err, "function invoke is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) GetFunctionInvocation(ctx context.Context, req *functionv1.GetFunctionInvocationRequest) (*functionv1.GetFunctionInvocationResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.GetFunctionInvocation", publicActionGetInvocation, withInvocationID(req.GetInvocationID()))
	var err error
	defer func() { op.End(err) }()

	if strings.TrimSpace(req.GetInvocationID()) == "" {
		err = grpcstatus.Error(codes.InvalidArgument, "invocation_id is required")
		return nil, err
	}
	resp, err := s.deps.Functions.GetFunctionInvocation(ctx, req)
	if err != nil {
		err = functionError(err, "function invocation get is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) ListFunctionInvocations(ctx context.Context, req *functionv1.ListFunctionInvocationsRequest) (*functionv1.ListFunctionInvocationsResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.ListFunctionInvocations", publicActionListInvocations)
	var err error
	defer func() { op.End(err) }()

	resp, err := s.deps.Functions.ListFunctionInvocations(ctx, req)
	if err != nil {
		err = functionError(err, "function invocation list is not implemented")
		return nil, err
	}
	return resp, nil
}

func (s *Server) ListFunctionEvents(ctx context.Context, req *functionv1.ListFunctionEventsRequest) (*functionv1.ListFunctionEventsResponse, error) {
	if s.deps.Functions == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "function control is unavailable")
	}
	ctx, op := publicOps.Function(ctx, "FunctionControl.ListFunctionEvents", publicActionListEvents, withFunctionID(req.GetFunctionID()))
	var err error
	defer func() { op.End(err) }()

	if strings.TrimSpace(req.GetFunctionID()) == "" && strings.TrimSpace(req.GetInvocationID()) == "" && strings.TrimSpace(req.GetRevisionID()) == "" {
		err = grpcstatus.Error(codes.InvalidArgument, "function_id, invocation_id, or revision_id is required")
		return nil, err
	}
	resp, err := s.deps.Functions.ListFunctionEvents(ctx, req)
	if err != nil {
		err = functionError(err, "function event list is not implemented")
		return nil, err
	}
	return resp, nil
}

func functionError(err error, message string) error {
	if functionkernel.IsNotImplemented(err) {
		return grpcstatus.Error(codes.Unimplemented, message)
	}
	if functionkernel.IsNotFound(err) {
		return grpcstatus.Error(codes.NotFound, "function resource not found")
	}
	return err
}

func validateDeployFunctionRequest(req *functionv1.DeployFunctionRequest) error {
	if req.GetWaitReady() {
		return grpcstatus.Error(codes.Unimplemented, "function deploy wait_ready is not implemented")
	}
	if name := strings.TrimSpace(req.GetName()); name == "" {
		return grpcstatus.Error(codes.InvalidArgument, "name is required")
	} else if !isStableFunctionName(name) {
		return grpcstatus.Error(codes.InvalidArgument, "name must start with a letter or digit and contain only letters, digits, '.', '_', or '-'")
	}
	spec := req.GetSpec()
	if spec == nil {
		return grpcstatus.Error(codes.InvalidArgument, "spec is required")
	}
	if strings.TrimSpace(spec.GetRuntime()) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "spec.runtime is required")
	}
	if strings.TrimSpace(spec.GetHandler()) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "spec.handler is required")
	}
	if err := validateFunctionWorkerSource(req.GetNamespace(), spec.GetWorkerSource()); err != nil {
		return err
	}
	if req.GetSource() == nil || req.GetSource().GetSource() == nil {
		return grpcstatus.Error(codes.InvalidArgument, "source is required")
	}
	if err := validateFunctionSource(req.GetSource()); err != nil {
		return err
	}
	if err := validateFunctionScaling(spec.GetScaling()); err != nil {
		return err
	}
	if err := validateExecutionConfigSecretRefs(spec.GetConfig()); err != nil {
		return err
	}
	if err := validateExecutionConfigResources(spec.GetConfig()); err != nil {
		return err
	}
	if err := validateExecutionConfigImageMounts(spec.GetConfig()); err != nil {
		return err
	}
	if err := validateServiceVolumeMounts(spec.GetConfig()); err != nil {
		return err
	}
	if len(spec.GetConfig().GetArgv()) > 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.config.argv is owned by the function worker")
	}
	return nil
}

func validateFunctionWorkerSource(namespace string, source *functionv1.FunctionWorkerSource) error {
	if source == nil {
		return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source is required")
	}
	switch typed := source.GetSource().(type) {
	case *functionv1.FunctionWorkerSource_EnvironmentID:
		if strings.TrimSpace(typed.EnvironmentID) == "" {
			return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source.environment_id is required")
		}
	case *functionv1.FunctionWorkerSource_Environment:
		if typed.Environment == nil {
			return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source.environment is required")
		}
		environmentNamespace := strings.TrimSpace(typed.Environment.GetNamespace())
		if environmentNamespace != "" && environmentNamespace != strings.TrimSpace(namespace) {
			return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source.environment.namespace must match the function namespace")
		}
		if hasTemplate := strings.TrimSpace(typed.Environment.GetTemplateID()) != ""; hasTemplate == (typed.Environment.GetImage() != nil) {
			return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source.environment must select exactly one template or image")
		}
	default:
		return grpcstatus.Error(codes.InvalidArgument, "spec.worker_source is required")
	}
	return nil
}

func validateUploadFunctionBundleOpen(open *functionv1.UploadFunctionBundleOpen) error {
	if name := strings.TrimSpace(open.GetName()); name == "" {
		return grpcstatus.Error(codes.InvalidArgument, "name is required")
	} else if !isStableFunctionName(name) {
		return grpcstatus.Error(codes.InvalidArgument, "name must start with a letter or digit and contain only letters, digits, '.', '_', or '-'")
	}
	if !isSHA256Digest(open.GetDigest()) {
		return grpcstatus.Error(codes.InvalidArgument, "digest must be sha256:<64 lowercase hex chars>")
	}
	if strings.TrimSpace(open.GetMediaType()) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "media_type is required")
	}
	if open.GetSizeBytes() <= 0 {
		return grpcstatus.Error(codes.InvalidArgument, "size_bytes must be greater than 0")
	}
	if open.GetSizeBytes() > maxFunctionBundleBytes {
		return grpcstatus.Errorf(codes.InvalidArgument, "size_bytes must be at most %d", maxFunctionBundleBytes)
	}
	return nil
}

func validateFunctionSource(source *functionv1.FunctionSource) error {
	switch src := source.GetSource().(type) {
	case *functionv1.FunctionSource_Bundle:
		if src.Bundle == nil {
			return grpcstatus.Error(codes.InvalidArgument, "source.bundle is required")
		}
		if !isSHA256Digest(src.Bundle.GetDigest()) {
			return grpcstatus.Error(codes.InvalidArgument, "source.bundle.digest must be sha256:<64 lowercase hex chars>")
		}
		if strings.TrimSpace(src.Bundle.GetStorageUri()) == "" {
			return grpcstatus.Error(codes.InvalidArgument, "source.bundle.storage_uri is required")
		}
		if !strings.HasPrefix(strings.TrimSpace(src.Bundle.GetStorageUri()), functionkernel.FunctionBundleStorageURIPrefix) {
			return grpcstatus.Error(codes.InvalidArgument, "source.bundle.storage_uri must come from UploadFunctionBundle")
		}
	case *functionv1.FunctionSource_Image:
		_ = src
		return grpcstatus.Error(codes.InvalidArgument, "source.image is not supported for function deploy; use UploadFunctionBundle")
	default:
		return grpcstatus.Error(codes.InvalidArgument, "source is required")
	}
	return nil
}

func validateFunctionScaling(scaling *functionv1.FunctionScalingSpec) error {
	if scaling == nil {
		return nil
	}
	if scaling.GetMinReplicas() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.scaling.min_replicas must be greater than or equal to 0")
	}
	if scaling.GetMaxReplicas() <= 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.scaling.max_replicas must be greater than 0")
	}
	if scaling.GetMaxReplicas() < scaling.GetMinReplicas() {
		return grpcstatus.Error(codes.InvalidArgument, "spec.scaling.max_replicas must be greater than or equal to min_replicas")
	}
	if scaling.GetConcurrency() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.scaling.concurrency must be greater than or equal to 0")
	}
	return nil
}

func validateFunctionLookup(functionID, _, name string) error {
	if strings.TrimSpace(functionID) != "" {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "function_id or name is required")
	}
	return nil
}

func validateFunctionInvokeRequest(req *functionv1.InvokeFunctionRequest) error {
	if err := validateFunctionLookup(req.GetFunctionID(), req.GetNamespace(), req.GetName()); err != nil {
		return err
	}
	if req.GetMode() == functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_UNSPECIFIED {
		return grpcstatus.Error(codes.InvalidArgument, "mode is required")
	}
	return nil
}

func isStableFunctionName(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, r := range value {
		if isASCIILetter(r) || isASCIIDigit(r) {
			continue
		}
		if index > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return false
	}
	first := rune(value[0])
	return isASCIILetter(first) || isASCIIDigit(first)
}

func isSHA256Digest(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "sha256:") {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
