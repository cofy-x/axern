package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	obsmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type nodeLifecycleServer struct {
	nodelifecyclev1.UnimplementedNodeLifecycleServer
	svc     serviceLike
	nodeID  string
	targets *AllocationTargetRegistry
}

type serviceLike interface {
	Start(context.Context, *runtimev1.StartRequest) (*runtimev1.StartResponse, error)
	Delete(context.Context, *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error)
	List(context.Context, *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error)
	DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error
}

type workspacePreparationProvider interface {
	WorkspacePreparation(containerID string) *commonv1.WorkspacePreparationFacts
}

func (s *nodeLifecycleServer) DeleteVolume(ctx context.Context, req *nodelifecyclev1.DeleteVolumeRequest) (*nodelifecyclev1.DeleteVolumeResponse, error) {
	if strings.TrimSpace(req.GetClaimID()) == "" || strings.TrimSpace(req.GetBackendHandle()) == "" || req.GetBackend() == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		return nil, grpcstatus.Error(codes.InvalidArgument, "claim_id, backend, and backend_handle are required")
	}
	if strings.TrimSpace(req.GetNodeID()) != "" && strings.TrimSpace(req.GetNodeID()) != s.nodeID {
		return nil, grpcstatus.Error(codes.PermissionDenied, "volume node_id does not match this node")
	}
	if err := s.svc.DeleteVolume(ctx, req.GetClaimID(), req.GetBackend(), req.GetBackendHandle()); err != nil {
		return nil, err
	}
	return &nodelifecyclev1.DeleteVolumeResponse{}, nil
}

const (
	lifecycleOperationCreate = "create"
	lifecycleOperationDelete = "delete"

	lifecycleStageValidateRequest   = "validate_request"
	lifecycleStageBuildStartRequest = "build_start_request"
	lifecycleStageServiceStart      = "service_start"
	lifecycleStageBindTarget        = "bind_target"
	lifecycleStageResolveTarget     = "resolve_target"
	lifecycleStageServiceDelete     = "service_delete"
	lifecycleStageConfirmDeleted    = "confirm_deleted"
	lifecycleStageMarkDeleted       = "mark_deleted"
	lifecycleStageTotal             = "total"
)

func NewNodeLifecycleServer(svc serviceLike, nodeID string, targets *AllocationTargetRegistry) nodelifecyclev1.NodeLifecycleServer {
	return &nodeLifecycleServer{
		svc:     svc,
		nodeID:  nodeID,
		targets: targets,
	}
}

func (s *nodeLifecycleServer) CreateAllocation(ctx context.Context, req *nodelifecyclev1.CreateAllocationRequest) (*nodelifecyclev1.CreateAllocationResponse, error) {
	totalStarted := time.Now()
	runtimeClass := lifecycleRuntimeClass(req)
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
	if strings.TrimSpace(req.GetNodeID()) != "" && strings.TrimSpace(req.GetNodeID()) != s.nodeID {
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
	resp, err := s.svc.Start(ctx, startReq)
	if err != nil {
		resultErr = err
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageServiceStart, runtimeClass, stageStarted, err)
		return nil, err
	}
	if resp.GetCode() != 0 {
		resultErr = grpcstatus.Errorf(codes.Internal, "node start failed: %s", resp.GetMessage())
		recordLifecycleStage(lifecycleOperationCreate, lifecycleStageServiceStart, runtimeClass, stageStarted, resultErr)
		return nil, resultErr
	}
	recordLifecycleStage(lifecycleOperationCreate, lifecycleStageServiceStart, runtimeClass, stageStarted, nil)
	stageStarted = time.Now()
	if resp.GetID() != "" {
		s.targets.bind(req.GetAllocationID(), resp.GetID())
	}
	recordLifecycleStage(lifecycleOperationCreate, lifecycleStageBindTarget, runtimeClass, stageStarted, nil)
	var workspacePreparation *commonv1.WorkspacePreparationFacts
	if provider, ok := s.svc.(workspacePreparationProvider); ok {
		workspacePreparation = provider.WorkspacePreparation(resp.GetID())
	}
	return &nodelifecyclev1.CreateAllocationResponse{
		AllocationID:         req.GetAllocationID(),
		Attempt:              req.GetAttempt(),
		PublishedVolumes:     clonePublishedNodeVolumes(resp.GetPublishedVolumes()),
		WorkspacePreparation: workspacePreparation,
	}, nil
}

func lifecycleRuntimeClass(req *nodelifecyclev1.CreateAllocationRequest) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.GetConfig().GetRuntimeClass())
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
	if strings.TrimSpace(req.GetNodeID()) != "" && strings.TrimSpace(req.GetNodeID()) != s.nodeID {
		resultErr = grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, resultErr)
		return nil, resultErr
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageValidateRequest, "", stageStarted, nil)
	if s.targets.isDeleted(req.GetAllocationID()) {
		return &nodelifecyclev1.DeleteAllocationResponse{}, nil
	}
	stageStarted = time.Now()
	targetID := s.targets.resolve(req.GetAllocationID())
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageResolveTarget, "", stageStarted, nil)
	stageStarted = time.Now()
	resp, err := s.svc.Delete(ctx, &runtimev1.DeleteRequest{ID: targetID, Timeout: req.GetTimeoutSeconds()})
	if err != nil {
		if allocationDeleteNotFound(err) {
			recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, nil)
			stageStarted = time.Now()
			s.targets.markDeleted(req.GetAllocationID())
			recordLifecycleStage(lifecycleOperationDelete, lifecycleStageMarkDeleted, "", stageStarted, nil)
			return &nodelifecyclev1.DeleteAllocationResponse{}, nil
		}
		resultErr = err
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageServiceDelete, "", stageStarted, nil)
	stageStarted = time.Now()
	if err := s.confirmAllocationDeleted(ctx, targetID); err != nil {
		resultErr = err
		recordLifecycleStage(lifecycleOperationDelete, lifecycleStageConfirmDeleted, "", stageStarted, err)
		return nil, err
	}
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageConfirmDeleted, "", stageStarted, nil)
	stageStarted = time.Now()
	s.targets.markDeleted(req.GetAllocationID())
	recordLifecycleStage(lifecycleOperationDelete, lifecycleStageMarkDeleted, "", stageStarted, nil)
	return &nodelifecyclev1.DeleteAllocationResponse{
		VolumeReleaseObservations: cloneVolumeReleaseObservations(resp.GetVolumeReleaseObservations()),
	}, nil
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

func (s *nodeLifecycleServer) GetAllocationStatus(ctx context.Context, req *nodelifecyclev1.GetAllocationStatusRequest) (*nodelifecyclev1.GetAllocationStatusResponse, error) {
	if strings.TrimSpace(req.GetAllocationID()) == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if strings.TrimSpace(req.GetNodeID()) != "" && strings.TrimSpace(req.GetNodeID()) != s.nodeID {
		return nil, grpcstatus.Error(codes.PermissionDenied, "allocation node_id does not match this node")
	}
	if s.targets.isDeleted(req.GetAllocationID()) {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation %q not found", req.GetAllocationID())
	}
	resp, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{ID: s.targets.resolve(req.GetAllocationID())})
	if err != nil {
		return nil, err
	}
	if len(resp.GetContainers()) == 0 {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation %q not found", req.GetAllocationID())
	}
	container := resp.GetContainers()[0]
	return &nodelifecyclev1.GetAllocationStatusResponse{
		Status:        allocationStatusFromContainerState(container.GetState()),
		ExitCode:      container.GetExitCode(),
		ExitCodeKnown: container.GetState() == runtimev1.ContainerState_CONTAINER_EXITED,
		Message:       container.GetMessage(),
	}, nil
}

func allocationStartRequest(req *nodelifecyclev1.CreateAllocationRequest) (*runtimev1.StartRequest, error) {
	spec := req.GetConfig()
	if spec == nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "config is required")
	}
	runtimeClass := strings.TrimSpace(spec.GetRuntimeClass())
	if runtimeClass == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "config.runtime_class is required")
	}
	cwd := strings.TrimSpace(spec.GetCwd())
	rootfsConfig, err := lifecycleRootfsConfig(spec)
	if err != nil {
		return nil, err
	}
	runtimeTemplate := &runtimev1.RuntimeTemplate{
		Sandbox:     runtimeClass,
		Command:     append([]string(nil), spec.GetArgv()...),
		Cwd:         cwd,
		RuntimeEnvs: cloneStringMap(spec.GetEnv()),
		Mounts:      toRuntimeLifecycleMountsFromAllocation(spec.GetMounts()),
		Rootfs:      rootfsConfig,
		ExecutionProfile: cloneRuntimeExecutionProfile(
			spec.GetExecutionProfile(),
		),
	}
	runtimeTemplate.ID = stableRuntimeTemplateID(runtimeTemplate)
	if runtimeTemplate.ID == "" {
		return nil, grpcstatus.Error(codes.Internal, "build stable runtime template id")
	}
	return &runtimev1.StartRequest{
		RuntimeTemplate: runtimeTemplate,
		Resources:       toRuntimeLifecycleResources(spec.GetResources()),
		ContainerID:     req.GetAllocationID(),
		Ports:           lifecyclePortsToRuntime(spec.GetPorts()),
		Network:         lifecycleNetworkToRuntime(spec.GetNetwork()),
		ExtraConfig:     lifecycleExtraConfig(spec, req.GetAttempt()),
		Stdout:          spec.GetStdoutPath(),
		Stderr:          spec.GetStderrPath(),
		NodeVolumes:     cloneResolvedNodeVolumes(spec.GetNodeVolumes()),
		ImageMounts:     cloneImageMounts(spec.GetImageMounts()),
		WorkspaceImage:  cloneWorkspaceImage(spec.GetWorkspaceImage()),
	}, nil
}

func resolvedSandboxStartRequest(containerID string, spec *nodelifecyclev1.ResolvedExecutionConfig) (*runtimev1.StartRequest, error) {
	sandboxRuntime := strings.TrimSpace(spec.GetRuntimeClass())
	if sandboxRuntime == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "config.runtime_class is required")
	}
	cwd := strings.TrimSpace(spec.GetCwd())

	rootfsConfig, err := lifecycleRootfsConfig(spec)
	if err != nil {
		return nil, err
	}

	runtimeTemplate := &runtimev1.RuntimeTemplate{
		Sandbox:     sandboxRuntime,
		Command:     append([]string(nil), spec.GetArgv()...),
		Cwd:         cwd,
		RuntimeEnvs: cloneStringMap(spec.GetEnv()),
		Mounts:      toRuntimeLifecycleMounts(spec.GetMounts()),
		Rootfs:      rootfsConfig,
		ExecutionProfile: cloneRuntimeExecutionProfile(
			spec.GetExecutionProfile(),
		),
	}
	runtimeTemplate.ID = stableRuntimeTemplateID(runtimeTemplate)
	if runtimeTemplate.ID == "" {
		return nil, grpcstatus.Error(codes.Internal, "build stable runtime template id")
	}
	return &runtimev1.StartRequest{
		RuntimeTemplate: runtimeTemplate,
		Resources:       toRuntimeLifecycleResources(spec.GetResources()),
		ContainerID:     containerID,
		Ports:           lifecyclePortsToRuntime(spec.GetPorts()),
		Network:         lifecycleNetworkToRuntime(spec.GetNetwork()),
		ExtraConfig:     lifecycleExtraConfig(spec, 0),
		Stdout:          spec.GetStdoutPath(),
		Stderr:          spec.GetStderrPath(),
		NodeVolumes:     cloneResolvedNodeVolumes(spec.GetNodeVolumes()),
		ImageMounts:     cloneImageMounts(spec.GetImageMounts()),
		WorkspaceImage:  cloneWorkspaceImage(spec.GetWorkspaceImage()),
	}, nil
}

func cloneRuntimeExecutionProfile(in *catalogv1.RuntimeExecutionProfile) *catalogv1.RuntimeExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*catalogv1.RuntimeExecutionProfile)
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
	case spec.GetS3Rootfs() != nil:
		rootfsConfig.Type = runtimev1.RootfsSrcType_S3
		rootfsConfig.Source = &runtimev1.RootfsConfig_S3Config{S3Config: &runtimev1.S3Config{
			Endpoint:        spec.GetS3Rootfs().GetEndpoint(),
			Bucket:          spec.GetS3Rootfs().GetBucket(),
			Object:          spec.GetS3Rootfs().GetObject(),
			AccessKeyID:     spec.GetS3Rootfs().GetAccessKeyID(),
			AccessKeySecret: spec.GetS3Rootfs().GetAccessKeySecret(),
		}}
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "one of config.image_descriptor, config.image_digest, config.local_rootfs_path, or config.s3_rootfs is required")
	}
	return rootfsConfig, nil
}

func allocationStatusFromContainerState(state runtimev1.ContainerState) commonv1.AllocationStatus {
	switch state {
	case runtimev1.ContainerState_CONTAINER_RUNNING:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING
	case runtimev1.ContainerState_CONTAINER_EXITED:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED
	default:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	}
}

func stableRuntimeTemplateID(template *runtimev1.RuntimeTemplate) string {
	if template == nil {
		return ""
	}
	staticTemplate := proto.Clone(template).(*runtimev1.RuntimeTemplate)
	staticTemplate.ID = ""
	if s3 := staticTemplate.GetRootfs().GetS3Config(); s3 != nil {
		s3.AccessKeyID = ""
		s3.AccessKeySecret = ""
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(staticTemplate)
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

func cloneWorkspaceImage(in *nodelifecyclev1.WorkspaceImageSource) *runtimev1.WorkspaceImageSource {
	if in == nil {
		return nil
	}
	out := &runtimev1.WorkspaceImageSource{SourcePath: strings.TrimSpace(in.GetSourcePath()), Target: strings.TrimSpace(in.GetTarget())}
	for _, variant := range in.GetVariants() {
		if variant == nil {
			continue
		}
		out.Variants = append(out.Variants, &runtimev1.WorkspaceImageVariant{Format: strings.TrimSpace(variant.GetFormat()), Image: strings.TrimSpace(variant.GetImage())})
	}
	return out
}

func cloneResolvedNodeVolumes(in []*privatestoragev1.ResolvedNodeVolume) []*privatestoragev1.ResolvedNodeVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.ResolvedNodeVolume, 0, len(in))
	for _, volume := range in {
		if volume == nil {
			continue
		}
		out = append(out, proto.Clone(volume).(*privatestoragev1.ResolvedNodeVolume))
	}
	return out
}

func clonePublishedNodeVolumes(in []*privatestoragev1.PublishedNodeVolume) []*privatestoragev1.PublishedNodeVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.PublishedNodeVolume, 0, len(in))
	for _, volume := range in {
		if volume == nil {
			continue
		}
		out = append(out, proto.Clone(volume).(*privatestoragev1.PublishedNodeVolume))
	}
	return out
}

func cloneVolumeReleaseObservations(in []*privatestoragev1.VolumeReleaseObservation) []*privatestoragev1.VolumeReleaseObservation {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.VolumeReleaseObservation, 0, len(in))
	for _, observation := range in {
		if observation == nil {
			continue
		}
		out = append(out, proto.Clone(observation).(*privatestoragev1.VolumeReleaseObservation))
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

func lifecyclePortsToRuntime(in []*commonv1.PortSpec) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, port := range in {
		if port == nil {
			continue
		}
		protocol := strings.ToLower(strings.TrimPrefix(port.GetProtocol().String(), "PORT_PROTOCOL_"))
		if protocol == "" || protocol == "unspecified" {
			protocol = "tcp"
		}
		if port.GetHostPort() > 0 {
			out = append(out, protocol+":"+itoa32(port.GetHostPort())+":"+itoa32(port.GetContainerPort()))
			continue
		}
		if port.GetContainerPort() > 0 {
			out = append(out, protocol+":"+itoa32(port.GetContainerPort())+":"+itoa32(port.GetContainerPort()))
		}
	}
	return out
}

func lifecycleNetworkToRuntime(in *commonv1.NetworkSpec) string {
	if in == nil || in.GetMode() == commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(in.GetMode().String(), "NETWORK_MODE_"))
}

func itoa32(value int32) string {
	return strconv.Itoa(int(value))
}

func toRuntimeLifecycleResources(in *commonv1.ResourceSpec) *commonv1.ResourceSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.ResourceSpec)
}

type lifecycleProbeJSON struct {
	Http *struct {
		Port   int32  `json:"port,omitempty"`
		Path   string `json:"path,omitempty"`
		Scheme string `json:"scheme,omitempty"`
	} `json:"http,omitempty"`
	Tcp *struct {
		Port int32 `json:"port,omitempty"`
	} `json:"tcp,omitempty"`
	InitialDelayMilliseconds int64 `json:"initialDelayMilliseconds,omitempty"`
	PeriodMilliseconds       int64 `json:"periodMilliseconds,omitempty"`
	TimeoutMilliseconds      int64 `json:"timeoutMilliseconds,omitempty"`
	SuccessThreshold         int32 `json:"successThreshold,omitempty"`
	FailureThreshold         int32 `json:"failureThreshold,omitempty"`
}

func lifecycleExtraConfig(spec *nodelifecyclev1.ResolvedExecutionConfig, attempt int64) string {
	if attempt <= 0 &&
		strings.TrimSpace(spec.GetNamespace()) == "" &&
		strings.TrimSpace(spec.GetServiceID()) == "" &&
		len(spec.GetLinuxCapabilities()) == 0 &&
		len(spec.GetSecretEnv()) == 0 &&
		len(spec.GetSecretFiles()) == 0 &&
		strings.TrimSpace(spec.GetRegistryCredential().GetDockerConfigJson()) == "" &&
		spec.GetReadinessProbe() == nil &&
		spec.GetLivenessProbe() == nil {
		return ""
	}
	payload := struct {
		LinuxCapabilities []string            `json:"linuxCapabilities,omitempty"`
		DockerConfigJSON  string              `json:"dockerConfigJson,omitempty"`
		Namespace         string              `json:"namespace,omitempty"`
		ServiceID         string              `json:"serviceId,omitempty"`
		AllocationAttempt int64               `json:"allocationAttempt,omitempty"`
		ReadinessProbe    *lifecycleProbeJSON `json:"readinessProbe,omitempty"`
		LivenessProbe     *lifecycleProbeJSON `json:"livenessProbe,omitempty"`
		SecretEnv         []struct {
			Name  string `json:"name,omitempty"`
			Value string `json:"value,omitempty"`
		} `json:"secretEnv,omitempty"`
		SecretFiles []struct {
			Path    string `json:"path,omitempty"`
			Content string `json:"content,omitempty"`
			Mode    uint32 `json:"mode,omitempty"`
		} `json:"secretFiles,omitempty"`
	}{
		LinuxCapabilities: append([]string(nil), spec.GetLinuxCapabilities()...),
		DockerConfigJSON:  strings.TrimSpace(spec.GetRegistryCredential().GetDockerConfigJson()),
		Namespace:         strings.TrimSpace(spec.GetNamespace()),
		ServiceID:         strings.TrimSpace(spec.GetServiceID()),
		AllocationAttempt: attempt,
	}
	for _, item := range spec.GetSecretEnv() {
		if item == nil {
			continue
		}
		payload.SecretEnv = append(payload.SecretEnv, struct {
			Name  string `json:"name,omitempty"`
			Value string `json:"value,omitempty"`
		}{Name: item.GetName(), Value: item.GetValue()})
	}
	for _, item := range spec.GetSecretFiles() {
		if item == nil {
			continue
		}
		payload.SecretFiles = append(payload.SecretFiles, struct {
			Path    string `json:"path,omitempty"`
			Content string `json:"content,omitempty"`
			Mode    uint32 `json:"mode,omitempty"`
		}{
			Path:    item.GetPath(),
			Content: base64.StdEncoding.EncodeToString(item.GetContent()),
			Mode:    item.GetMode(),
		})
	}
	payload.ReadinessProbe = lifecycleProbePayload(spec.GetReadinessProbe())
	payload.LivenessProbe = lifecycleProbePayload(spec.GetLivenessProbe())
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func lifecycleProbePayload(probe *nodelifecyclev1.ResolvedProbe) *lifecycleProbeJSON {
	if probe == nil {
		return nil
	}
	payload := &lifecycleProbeJSON{
		InitialDelayMilliseconds: durationMilliseconds(probe.GetInitialDelay()),
		PeriodMilliseconds:       durationMilliseconds(probe.GetPeriod()),
		TimeoutMilliseconds:      durationMilliseconds(probe.GetTimeout()),
		SuccessThreshold:         probe.GetSuccessThreshold(),
		FailureThreshold:         probe.GetFailureThreshold(),
	}
	if http := probe.GetHttp(); http != nil {
		payload.Http = &struct {
			Port   int32  `json:"port,omitempty"`
			Path   string `json:"path,omitempty"`
			Scheme string `json:"scheme,omitempty"`
		}{
			Port:   http.GetPort(),
			Path:   http.GetPath(),
			Scheme: strings.ToLower(strings.TrimPrefix(http.GetScheme().String(), "HTTP_PROBE_SCHEME_")),
		}
	}
	if tcp := probe.GetTcp(); tcp != nil {
		payload.Tcp = &struct {
			Port int32 `json:"port,omitempty"`
		}{Port: tcp.GetPort()}
	}
	return payload
}

func durationMilliseconds(value *durationpb.Duration) int64 {
	if value == nil {
		return 0
	}
	return value.AsDuration().Milliseconds()
}
