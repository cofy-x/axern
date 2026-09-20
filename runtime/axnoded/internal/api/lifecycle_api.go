package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/lib/go/executionlease"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	obsmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type nodeLifecycleServer struct {
	nodelifecyclev1.UnimplementedNodeLifecycleServer
	svc        serviceLike
	nodeID     string
	allowLocal bool
}

type serviceLike interface {
	Start(context.Context, *runtimev1.StartRequest) (*runtimev1.StartResponse, error)
	Delete(context.Context, *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error)
	StartControlPlaneAllocation(context.Context, string, *runtimev1.StartRequest) (*runtimev1.StartResponse, error)
	DeleteControlPlaneAllocation(context.Context, string, *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error)
	AcknowledgeControlPlaneAllocationRelease(string, string) error
	HasControlPlaneAllocation(string, string) bool
	IsControlPlaneAllocation(string) bool
	List(context.Context, *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error)
	ReconcileAllocationCapabilities(context.Context, string) ([]*capabilityv1.CapabilityRequirement, *capabilityv1.CapabilityConditionSet, error)
}

const (
	lifecycleOperationCreate = "create"
	lifecycleOperationDelete = "delete"

	lifecycleStageValidateRequest   = "validate_request"
	lifecycleStageBuildStartRequest = "build_start_request"
	lifecycleStageServiceStart      = "service_start"
	lifecycleStageServiceDelete     = "service_delete"
	lifecycleStageConfirmDeleted    = "confirm_deleted"
	lifecycleStageTotal             = "total"
)

func NewNodeLifecycleServer(svc serviceLike, nodeID string) nodelifecyclev1.NodeLifecycleServer {
	return &nodeLifecycleServer{
		svc:    svc,
		nodeID: nodeID,
	}
}

// NewLocalNodeLifecycleServer exposes the same lifecycle protocol on the
// privileged node-local socket for runtime conformance verification. Local
// allocations are deliberately unbound and never acquire control-plane
// authority or enter node inventory.
func NewLocalNodeLifecycleServer(svc serviceLike, nodeID string) nodelifecyclev1.NodeLifecycleServer {
	return &nodeLifecycleServer{
		svc:        svc,
		nodeID:     nodeID,
		allowLocal: true,
	}
}

func (s *nodeLifecycleServer) CreateAllocation(ctx context.Context, req *nodelifecyclev1.CreateAllocationRequest) (*nodelifecyclev1.CreateAllocationResponse, error) {
	totalStarted := time.Now()
	runtimeClass := "runsc"
	var resultErr error
	defer func() {
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageTotal, runtimeClass, totalStarted, resultErr)
	}()
	stageStarted := time.Now()
	if strings.TrimSpace(req.GetAllocationID()) == "" {
		resultErr = grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	requestNodeID := strings.TrimSpace(req.GetNodeID())
	leaseTTL := time.Duration(req.GetExecutionLeaseTtlSeconds()) * time.Second
	if s.allowLocal && requestNodeID != "" {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint accepts only unbound local Allocations")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	if s.allowLocal && s.svc.IsControlPlaneAllocation(req.GetAllocationID()) {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot create or replace a control-plane-bound Allocation")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID == "" && !s.allowLocal {
		resultErr = grpcstatus.Error(codes.InvalidArgument, "node_id is required on the control-plane lifecycle endpoint")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID == "" && leaseTTL != 0 {
		resultErr = grpcstatus.Error(codes.InvalidArgument, "node-local allocation cannot carry an execution lease")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID != "" && (leaseTTL <= 0 || leaseTTL > executionlease.TTL) {
		resultErr = grpcstatus.Errorf(codes.InvalidArgument, "execution_lease_ttl_seconds must be between 1 and %d for a node-bound allocation", int64(executionlease.TTL/time.Second))
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID != "" && requestNodeID != s.nodeID {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	recordLifecycleStage(lifecycleOperationCreate, lifecycleStageValidateRequest, runtimeClass, stageStarted, nil)
	stageStarted = time.Now()
	startReq, err := allocationStartRequest(req)
	if err != nil {
		resultErr = err
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageBuildStartRequest, runtimeClass, stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationCreate, lifecycleStageBuildStartRequest, runtimeClass, stageStarted, nil)
	stageStarted = time.Now()
	var resp *runtimev1.StartResponse
	if requestNodeID == "" {
		resp, err = s.svc.Start(ctx, startReq)
	} else {
		resp, err = s.svc.StartControlPlaneAllocation(ctx, requestNodeID, startReq)
	}
	if err != nil {
		resultErr = err
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageServiceStart, runtimeClass, stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationCreate, lifecycleStageServiceStart, runtimeClass, stageStarted, nil)
	if resp.GetAllocationID() != req.GetAllocationID() {
		resultErr = grpcstatus.Errorf(codes.Internal, "node start returned execution id %q for allocation %q", resp.GetAllocationID(), req.GetAllocationID())
		return nil, resultErr
	}
	return &nodelifecyclev1.CreateAllocationResponse{
		AllocationID:           req.GetAllocationID(),
		CapabilityVerification: cloneCapabilityConditionSet(resp.GetCapabilityVerification()),
	}, nil
}

func recordLifecycleStage(operation, stage, runtimeClass string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = lifecycleErrorClass(err)
	}
	obsmetrics.RecordLifecycleStageDuration(operation, stage, runtimeClass, result, errorClass, time.Since(started).Seconds())
}

func lifecycleErrorClass(err error) string {
	if err == nil {
		return ""
	}
	code := grpcstatus.Code(err)
	if code != codes.OK && code != codes.Unknown {
		return strings.ToLower(code.String())
	}
	return "error"
}

func (s *nodeLifecycleServer) DeleteAllocation(ctx context.Context, req *nodelifecyclev1.DeleteAllocationRequest) (*nodelifecyclev1.DeleteAllocationResponse, error) {
	totalStarted := time.Now()
	var resultErr error
	defer func() {
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageTotal, "", totalStarted, resultErr)
	}()
	stageStarted := time.Now()
	if strings.TrimSpace(req.GetAllocationID()) == "" {
		resultErr = grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	requestNodeID := strings.TrimSpace(req.GetNodeID())
	if s.allowLocal && requestNodeID != "" {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot delete control-plane-bound Allocations")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	if s.allowLocal && s.svc.IsControlPlaneAllocation(req.GetAllocationID()) {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot delete a control-plane-bound Allocation")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID == "" && !s.allowLocal {
		resultErr = grpcstatus.Error(codes.InvalidArgument, "node_id is required on the control-plane lifecycle endpoint")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	if requestNodeID != "" && requestNodeID != s.nodeID {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, nil)
	stageStarted = time.Now()
	deleteRequest := &runtimev1.DeleteRequest{ID: req.GetAllocationID(), Timeout: req.GetTimeoutSeconds()}
	if outputSealing := req.GetOutputSealing(); outputSealing != nil {
		deleteRequest.OutputSealing = &runtimev1.OutputSealingRequest{
			ExpiresAtUnixNano: outputSealing.GetExpiresAtUnixNano(),
			Outputs:           cloneDeclaredOutputs(outputSealing.GetOutputs()),
		}
	}
	if snapshot := req.GetRootfsSnapshotSealing(); snapshot != nil {
		credentialJSON := ""
		if credential := snapshot.GetBaseRegistryCredential(); credential != nil {
			credentialJSON = credential.GetDockerConfigJson()
		}
		deleteRequest.RootfsSnapshotSealing = &runtimev1.RootfsSnapshotSealingRequest{
			BaseImageRef: snapshot.GetBaseImageRef(), BaseRegistryCredentialJson: credentialJSON,
		}
	}
	var response *runtimev1.DeleteResponse
	var err error
	if requestNodeID == "" {
		response, err = s.svc.Delete(ctx, deleteRequest)
	} else {
		response, err = s.svc.DeleteControlPlaneAllocation(ctx, requestNodeID, deleteRequest)
	}
	if err != nil {
		if allocationDeleteNotFound(err) {
			recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, nil)
			return &nodelifecyclev1.DeleteAllocationResponse{}, nil
		}
		resultErr = err
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, nil)
	stageStarted = time.Now()
	if err := s.confirmAllocationDeleted(ctx, req.GetAllocationID()); err != nil {
		resultErr = err
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageConfirmDeleted, "", stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageConfirmDeleted, "", stageStarted, nil)
	result := &nodelifecyclev1.DeleteAllocationResponse{}
	if snapshot := response.GetRootfsSnapshot(); snapshot != nil {
		result.RootfsSnapshot = &nodelifecyclev1.RootfsSnapshotSealingResult{
			ImageRef: snapshot.GetImageRef(), ImageDescriptor: cloneOCIImageDescriptor(snapshot.GetImageDescriptor()),
			PlatformOS: snapshot.GetPlatformOS(), PlatformArch: snapshot.GetPlatformArch(), PlatformVariant: snapshot.GetPlatformVariant(),
		}
	}
	return result, nil
}

func (s *nodeLifecycleServer) AcknowledgeAllocationRelease(_ context.Context, req *nodelifecyclev1.AcknowledgeAllocationReleaseRequest) (*nodelifecyclev1.AcknowledgeAllocationReleaseResponse, error) {
	if s.allowLocal {
		return nil, grpcstatus.Error(codes.PermissionDenied, "release acknowledgement is control-plane only")
	}
	allocationID, nodeID := strings.TrimSpace(req.GetAllocationID()), strings.TrimSpace(req.GetNodeID())
	if allocationID == "" || nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id and node_id are required")
	}
	if nodeID != s.nodeID {
		return nil, grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
	}
	if err := s.svc.AcknowledgeControlPlaneAllocationRelease(allocationID, nodeID); err != nil {
		return nil, err
	}
	return &nodelifecyclev1.AcknowledgeAllocationReleaseResponse{}, nil
}

func cloneOCIImageDescriptor(in *environmentv1.OciImageDescriptor) *environmentv1.OciImageDescriptor {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*environmentv1.OciImageDescriptor)
}

func allocationDeleteNotFound(err error) bool {
	return grpcstatus.Code(err) == codes.NotFound
}

func (s *nodeLifecycleServer) confirmAllocationDeleted(ctx context.Context, targetID string) error {
	resp, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{ID: targetID})
	if grpcstatus.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if len(resp.GetContainers()) == 0 {
		return nil
	}
	return grpcstatus.Errorf(codes.Unavailable, "allocation %q still exists after delete", targetID)
}

func (s *nodeLifecycleServer) GetAllocationLifecycle(ctx context.Context, req *nodelifecyclev1.GetAllocationLifecycleRequest) (*nodelifecyclev1.GetAllocationLifecycleResponse, error) {
	if strings.TrimSpace(req.GetAllocationID()) == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	requestNodeID := strings.TrimSpace(req.GetNodeID())
	if s.allowLocal && requestNodeID != "" {
		return nil, grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot inspect control-plane-bound Allocations")
	}
	if s.allowLocal && s.svc.IsControlPlaneAllocation(req.GetAllocationID()) {
		return nil, grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot inspect a control-plane-bound Allocation")
	}
	if requestNodeID == "" && !s.allowLocal {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required on the control-plane lifecycle endpoint")
	}
	if requestNodeID != "" && requestNodeID != s.nodeID {
		return nil, grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
	}
	if requestNodeID != "" && !s.svc.HasControlPlaneAllocation(req.GetAllocationID(), requestNodeID) {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation %q is not admitted to this node", req.GetAllocationID())
	}
	resp, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{ID: req.GetAllocationID()})
	if err != nil {
		return nil, err
	}
	if len(resp.GetContainers()) == 0 {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation %q not found", req.GetAllocationID())
	}
	container := resp.GetContainers()[0]
	_, capabilityVerification, err := s.svc.ReconcileAllocationCapabilities(ctx, req.GetAllocationID())
	if err != nil {
		return nil, err
	}
	return &nodelifecyclev1.GetAllocationLifecycleResponse{
		State:                  allocationLifecycleStateFromContainerState(container.GetState()),
		ExitCode:               container.ExitCode,
		Message:                container.GetMessage(),
		DiagnosticCode:         container.GetDiagnosticCode(),
		CapabilityVerification: cloneCapabilityConditionSet(capabilityVerification),
	}, nil
}

func allocationStartRequest(req *nodelifecyclev1.CreateAllocationRequest) (*runtimev1.StartRequest, error) {
	spec := req.GetConfig()
	if spec == nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "config is required")
	}
	cwd := strings.TrimSpace(spec.GetCwd())
	rootfsConfig, err := lifecycleRootfsConfig(spec)
	if err != nil {
		return nil, err
	}
	preparedEnvironment := &runtimev1.ResolvedEnvironment{
		Argv:   append([]string(nil), spec.GetArgv()...),
		Cwd:    cwd,
		Env:    cloneStringMap(spec.GetEnv()),
		Mounts: toRuntimeLifecycleMountsFromAllocation(spec.GetMounts()),
		Rootfs: rootfsConfig,
		ExecutionProfile: cloneOciExecutionProfile(
			spec.GetExecutionProfile(),
		),
	}
	preparedEnvironment.ID = stableResolvedEnvironmentID(preparedEnvironment)
	if preparedEnvironment.ID == "" {
		return nil, grpcstatus.Error(codes.Internal, "build stable prepared environment id")
	}
	return &runtimev1.StartRequest{
		Environment:            preparedEnvironment,
		Resources:              toRuntimeLifecycleResources(spec.GetResources()),
		AllocationID:           req.GetAllocationID(),
		Network:                cloneNetworkSpec(spec.GetNetwork()),
		RegistryCredential:     cloneRegistryCredential(spec.GetRegistryCredential()),
		SecretEnv:              cloneResolvedSecretEnv(spec.GetSecretEnv()),
		SecretFiles:            cloneResolvedSecretFiles(spec.GetSecretFiles()),
		Stdout:                 spec.GetStdoutPath(),
		Stderr:                 spec.GetStderrPath(),
		ImageMounts:            cloneImageMounts(spec.GetImageMounts()),
		CapabilityRequirements: cloneCapabilityRequirements(spec.GetCapabilityRequirements()),
		DeclaredOutputs:        cloneDeclaredOutputs(spec.GetDeclaredOutputs()),
		RootfsSnapshot:         cloneRootfsSnapshot(spec.GetRootfsSnapshot()),
		ExtensionCapabilityRequirements: cloneExtensionCapabilityRequirements(
			spec.GetExtensionCapabilityRequirements(),
		),
		ExecutionLeaseTtlSeconds: req.GetExecutionLeaseTtlSeconds(),
	}, nil
}

func cloneRootfsSnapshot(in *commonv1.RootfsSnapshot) *commonv1.RootfsSnapshot {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.RootfsSnapshot)
}

func resolvedSandboxStartRequest(containerID string, spec *nodelifecyclev1.ResolvedExecutionConfig) (*runtimev1.StartRequest, error) {
	cwd := strings.TrimSpace(spec.GetCwd())

	rootfsConfig, err := lifecycleRootfsConfig(spec)
	if err != nil {
		return nil, err
	}

	preparedEnvironment := &runtimev1.ResolvedEnvironment{
		Argv:   append([]string(nil), spec.GetArgv()...),
		Cwd:    cwd,
		Env:    cloneStringMap(spec.GetEnv()),
		Mounts: toRuntimeLifecycleMounts(spec.GetMounts()),
		Rootfs: rootfsConfig,
		ExecutionProfile: cloneOciExecutionProfile(
			spec.GetExecutionProfile(),
		),
	}
	preparedEnvironment.ID = stableResolvedEnvironmentID(preparedEnvironment)
	if preparedEnvironment.ID == "" {
		return nil, grpcstatus.Error(codes.Internal, "build stable prepared environment id")
	}
	return &runtimev1.StartRequest{
		Environment:            preparedEnvironment,
		Resources:              toRuntimeLifecycleResources(spec.GetResources()),
		AllocationID:           containerID,
		Network:                cloneNetworkSpec(spec.GetNetwork()),
		RegistryCredential:     cloneRegistryCredential(spec.GetRegistryCredential()),
		SecretEnv:              cloneResolvedSecretEnv(spec.GetSecretEnv()),
		SecretFiles:            cloneResolvedSecretFiles(spec.GetSecretFiles()),
		Stdout:                 spec.GetStdoutPath(),
		Stderr:                 spec.GetStderrPath(),
		ImageMounts:            cloneImageMounts(spec.GetImageMounts()),
		CapabilityRequirements: cloneCapabilityRequirements(spec.GetCapabilityRequirements()),
		DeclaredOutputs:        cloneDeclaredOutputs(spec.GetDeclaredOutputs()),
		ExtensionCapabilityRequirements: cloneExtensionCapabilityRequirements(
			spec.GetExtensionCapabilityRequirements(),
		),
	}, nil
}

func cloneOciExecutionProfile(in *environmentv1.OciExecutionProfile) *environmentv1.OciExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*environmentv1.OciExecutionProfile)
}

func cloneCapabilityRequirements(in []*capabilityv1.CapabilityRequirement) []*capabilityv1.CapabilityRequirement {
	out := make([]*capabilityv1.CapabilityRequirement, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityRequirement))
		}
	}
	return out
}

func cloneDeclaredOutputs(in []*commonv1.DeclaredOutput) []*commonv1.DeclaredOutput {
	out := make([]*commonv1.DeclaredOutput, 0, len(in))
	for _, declared := range in {
		if declared != nil {
			out = append(out, proto.Clone(declared).(*commonv1.DeclaredOutput))
		}
	}
	return out
}

func cloneCapabilityConditionSet(in *capabilityv1.CapabilityConditionSet) *capabilityv1.CapabilityConditionSet {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilityConditionSet)
}

func cloneExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, requirement := range in {
		if requirement != nil {
			out = append(out, proto.Clone(requirement).(*capabilityv1.ExtensionCapabilityRequirement))
		}
	}
	return out
}

func lifecycleRootfsConfig(spec *nodelifecyclev1.ResolvedExecutionConfig) (*runtimev1.RootfsConfig, error) {
	rootfsConfig := &runtimev1.RootfsConfig{Readonly: spec.GetRootfsReadonly()}
	switch {
	case strings.TrimSpace(spec.GetImageDescriptor()) != "":
		rootfsConfig.Type = runtimev1.RootfsSrcType_IMAGE
		rootfsConfig.Source = &runtimev1.RootfsConfig_ImageUrl{ImageUrl: strings.TrimSpace(spec.GetImageDescriptor())}
	case strings.TrimSpace(spec.GetImageDigest()) != "":
		rootfsConfig.Type = runtimev1.RootfsSrcType_IMAGE
		rootfsConfig.Source = &runtimev1.RootfsConfig_ImageUrl{ImageUrl: strings.TrimSpace(spec.GetImageDigest())}
	case strings.TrimSpace(spec.GetLocalRootfsPath()) != "":
		rootfsConfig.Type = runtimev1.RootfsSrcType_LOCAL
		rootfsConfig.Source = &runtimev1.RootfsConfig_Path{Path: strings.TrimSpace(spec.GetLocalRootfsPath())}
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "one of config.image_descriptor, config.image_digest, or config.local_rootfs_path is required")
	}
	return rootfsConfig, nil
}

func allocationLifecycleStateFromContainerState(state runtimev1.ContainerState) commonv1.AllocationLifecycleState {
	switch state {
	case runtimev1.ContainerState_CONTAINER_RUNNING:
		return commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE
	case runtimev1.ContainerState_CONTAINER_EXITED:
		return commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED
	default:
		return commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED
	}
}

func stableResolvedEnvironmentID(environment *runtimev1.ResolvedEnvironment) string {
	if environment == nil {
		return ""
	}
	staticEnvironment := proto.Clone(environment).(*runtimev1.ResolvedEnvironment)
	staticEnvironment.ID = ""
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(staticEnvironment)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "rt-" + hex.EncodeToString(sum[:16])
}

func cloneImageMounts(in []*nodelifecyclev1.ImageMount) []*runtimev1.ImageMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*runtimev1.ImageMount, 0, len(in))
	for _, mount := range in {
		if mount == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &runtimev1.ImageMount{
			Image:    strings.TrimSpace(mount.GetImage()),
			Target:   strings.TrimSpace(mount.GetTarget()),
			Readonly: true,
		})
	}
	return out
}

func toRuntimeLifecycleMounts(in []*nodelifecyclev1.SandboxMount) []*runtimev1.Mount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*runtimev1.Mount, 0, len(in))
	for _, mount := range in {
		if mount == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &runtimev1.Mount{
			Type:    mount.GetType(),
			Source:  mount.GetSource(),
			Target:  mount.GetTarget(),
			Options: append([]string(nil), mount.GetOptions()...),
		})
	}
	return out
}

func toRuntimeLifecycleMountsFromAllocation(in []*nodelifecyclev1.SandboxMount) []*runtimev1.Mount {
	return toRuntimeLifecycleMounts(in)
}

func cloneNetworkSpec(in *commonv1.NetworkSpec) *commonv1.NetworkSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.NetworkSpec)
}

func toRuntimeLifecycleResources(in *commonv1.ResourceSpec) *commonv1.ResourceSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.ResourceSpec)
}

func cloneRegistryCredential(in *nodelifecyclev1.RegistryCredential) *runtimev1.RegistryCredential {
	if in == nil {
		return nil
	}
	return &runtimev1.RegistryCredential{DockerConfigJson: strings.TrimSpace(in.GetDockerConfigJson())}
}

func cloneResolvedSecretEnv(in []*nodelifecyclev1.ResolvedSecretEnvVar) []*runtimev1.ResolvedSecretEnvVar {
	out := make([]*runtimev1.ResolvedSecretEnvVar, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &runtimev1.ResolvedSecretEnvVar{Name: item.GetName(), Value: item.GetValue()})
	}
	return out
}

func cloneResolvedSecretFiles(in []*nodelifecyclev1.ResolvedSecretFile) []*runtimev1.ResolvedSecretFile {
	out := make([]*runtimev1.ResolvedSecretFile, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &runtimev1.ResolvedSecretFile{Path: item.GetPath(), Content: append([]byte(nil), item.GetContent()...), Mode: item.GetMode()})
	}
	return out
}
