package axernsdk

import (
	"context"
	"path"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// CreateEnvironmentOptions configures a control-plane environment.
type CreateEnvironmentOptions struct {
	Namespace            string
	TemplateID           string
	Image                string
	RegistryCredentialID string
	RootFSReadonly       bool
	Labels               map[string]string
}

// CreateEnvironment creates an Axern environment from a template or image.
func (c *Client) CreateEnvironment(ctx context.Context, options CreateEnvironmentOptions) (*environmentv1.Environment, error) {
	namespace := defaultString(options.Namespace, "default")
	sourceCount := countNonEmpty(options.TemplateID, options.Image)
	if sourceCount != 1 {
		return nil, ErrInvalidSource
	}
	spec := &environmentv1.EnvironmentSpec{Namespace: namespace}
	if options.TemplateID != "" {
		spec.TemplateID = options.TemplateID
	} else {
		spec.Image = &environmentv1.EnvironmentImageSource{
			Ref:                  options.Image,
			RegistryCredentialID: options.RegistryCredentialID,
			RootfsReadonly:       options.RootFSReadonly,
		}
	}
	response, err := c.environments.CreateEnvironment(ctx, &environmentv1.CreateEnvironmentRequest{
		Spec:   spec,
		Labels: cloneMap(options.Labels),
	})
	if err != nil {
		return nil, mapRPCError(err, "create environment", "")
	}
	return response.GetEnvironment(), nil
}

// DeleteEnvironment deletes an environment by id.
func (c *Client) DeleteEnvironment(ctx context.Context, environmentID string) error {
	_, err := c.environments.DeleteEnvironment(ctx, &environmentv1.DeleteEnvironmentRequest{EnvironmentID: environmentID})
	return mapRPCError(err, "delete environment", "")
}

// CreateServiceOptions configures a single-replica service for sandbox use.
type CreateServiceOptions struct {
	Namespace               string
	EnvironmentID           string
	Argv                    []string
	Env                     map[string]string
	Cwd                     string
	RuntimeClass            string
	Volumes                 []VolumeMount
	ImageMounts             []ImageMount
	WorkspaceImage          *WorkspaceImageSource
	RequestCPU              ResourceQuantity
	RequestMemory           ResourceQuantity
	RequestEphemeralStorage ResourceQuantity
	LimitCPU                ResourceQuantity
	LimitMemory             ResourceQuantity
	LimitEphemeralStorage   ResourceQuantity
	Labels                  map[string]string
}

// CreateService creates an Axern service.
func (c *Client) CreateService(ctx context.Context, options CreateServiceOptions) (*servicev1.Service, error) {
	if options.EnvironmentID == "" {
		return nil, requiredError("environment_id")
	}
	if err := validateImageMounts(options.ImageMounts); err != nil {
		return nil, err
	}
	if err := validateWorkspaceImage(options.WorkspaceImage); err != nil {
		return nil, err
	}
	if err := validateWorkspaceImageMounts(options.WorkspaceImage, options.ImageMounts, options.Volumes); err != nil {
		return nil, err
	}
	resources, err := buildResourceSpec(options.RequestCPU, options.RequestMemory, options.RequestEphemeralStorage, options.LimitCPU, options.LimitMemory, options.LimitEphemeralStorage)
	if err != nil {
		return nil, err
	}
	response, err := c.services.CreateService(ctx, &servicev1.CreateServiceRequest{
		Namespace:     defaultString(options.Namespace, "default"),
		EnvironmentID: options.EnvironmentID,
		Replicas:      1,
		Config: &commonv1.ExecutionConfig{
			Argv:           append([]string(nil), options.Argv...),
			Env:            cloneMap(options.Env),
			Cwd:            options.Cwd,
			RuntimeClass:   options.RuntimeClass,
			VolumeMounts:   serviceVolumeMounts(options.Volumes),
			ImageMounts:    executionImageMounts(options.ImageMounts),
			WorkspaceImage: executionWorkspaceImage(options.WorkspaceImage),
			Resources:      resources,
		},
		Labels: cloneMap(options.Labels),
	})
	if err != nil {
		return nil, mapRPCError(err, "create service", "")
	}
	return response.GetService(), nil
}

// VolumeMount describes a service volume claim mounted into a sandbox.
type VolumeMount struct {
	Name     string
	Target   string
	Readonly bool
	Options  []string
}

// ImageMount describes a read-only OCI image mounted into the workload rootfs.
type ImageMount struct {
	Image    string
	Target   string
	Readonly bool
}

// WorkspaceImageSource describes an immutable TaskSet payload mounted through
// an allocation-local copy-on-write view. Variants are ordered by preference.
type WorkspaceImageSource struct {
	Variants   []WorkspaceImageVariant
	SourcePath string
	Target     string
}

type WorkspaceImageVariant struct{ Format, Image string }

func executionWorkspaceImage(source *WorkspaceImageSource) *commonv1.WorkspaceImageSource {
	if source == nil {
		return nil
	}
	out := &commonv1.WorkspaceImageSource{SourcePath: source.SourcePath, Target: defaultString(source.Target, "/workspace")}
	for _, variant := range source.Variants {
		out.Variants = append(out.Variants, &commonv1.WorkspaceImageVariant{Format: variant.Format, Image: variant.Image})
	}
	return out
}

func validateWorkspaceImage(source *WorkspaceImageSource) error {
	if source == nil {
		return nil
	}
	if len(source.Variants) == 0 {
		return validationError("workspace_image.variants", "must not be empty")
	}
	seenFormats := map[string]bool{}
	for _, variant := range source.Variants {
		if variant.Format != "nydus" && variant.Format != "oci" {
			return validationError("workspace_image.variants.format", "must be nydus or oci")
		}
		if strings.TrimSpace(variant.Image) == "" {
			return validationError("workspace_image.variants.image", "is required")
		}
		if !isImmutableSHA256Reference(variant.Image) {
			return validationError("workspace_image.variants.image", "must use an immutable sha256 digest reference")
		}
		if seenFormats[variant.Format] {
			return validationError("workspace_image.variants.format", "must not be duplicated")
		}
		seenFormats[variant.Format] = true
	}
	sourcePath := path.Clean(strings.TrimSpace(source.SourcePath))
	parts := strings.Split(sourcePath, "/")
	if len(parts) != 3 || parts[0] != "tasks" || parts[1] == "" || parts[2] != "workspace" || pathHasParentReference(source.SourcePath) {
		return validationError("workspace_image.source_path", "must select tasks/<id>/workspace")
	}
	target := path.Clean(defaultString(source.Target, "/workspace"))
	if target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(source.Target) {
		return validationError("workspace_image.target", "must be an absolute path below /")
	}
	if protectedImageMountTarget(target) {
		return validationError("workspace_image.target", "is a protected system path")
	}
	return nil
}

func isImmutableSHA256Reference(value string) bool {
	_, digest, ok := strings.Cut(strings.TrimSpace(value), "@sha256:")
	if !ok || len(digest) != 64 {
		return false
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validateWorkspaceImageMounts(source *WorkspaceImageSource, imageMounts []ImageMount, volumeMounts []VolumeMount) error {
	if source == nil {
		return nil
	}
	target := path.Clean(defaultString(source.Target, "/workspace"))
	for _, mount := range imageMounts {
		mountTarget := path.Clean(strings.TrimSpace(mount.Target))
		if target == mountTarget || strings.HasPrefix(target, mountTarget+"/") || strings.HasPrefix(mountTarget, target+"/") {
			return validationError("workspace_image.target", "must not overlap image_mounts")
		}
	}
	for _, mount := range volumeMounts {
		mountTarget := path.Clean(strings.TrimSpace(mount.Target))
		if target == mountTarget || strings.HasPrefix(target, mountTarget+"/") || strings.HasPrefix(mountTarget, target+"/") {
			return validationError("workspace_image.target", "must not overlap volume mounts")
		}
	}
	return nil
}

func serviceVolumeMounts(mounts []VolumeMount) []*commonv1.ServiceVolumeMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*commonv1.ServiceVolumeMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &commonv1.ServiceVolumeMount{
			Name:     mount.Name,
			Target:   mount.Target,
			Readonly: mount.Readonly,
			Options:  append([]string(nil), mount.Options...),
		})
	}
	return out
}

func executionImageMounts(mounts []ImageMount) []*commonv1.ImageMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*commonv1.ImageMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &commonv1.ImageMount{
			Image:    mount.Image,
			Target:   mount.Target,
			Readonly: true,
		})
	}
	return out
}

func validateImageMounts(mounts []ImageMount) error {
	seenTargets := map[string]struct{}{}
	for _, mount := range mounts {
		if strings.TrimSpace(mount.Image) == "" {
			return validationError("image_mounts.image", "is required")
		}
		rawTarget := strings.TrimSpace(mount.Target)
		target := path.Clean(rawTarget)
		if target == "." || target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(rawTarget) {
			return validationError("image_mounts.target", "must be an absolute path below /")
		}
		if protectedImageMountTarget(target) {
			return validationError("image_mounts.target", "must not be a protected system path")
		}
		for existing := range seenTargets {
			if pathsOverlap(existing, target) {
				return validationError("image_mounts.target", "must not overlap another image mount target")
			}
		}
		seenTargets[target] = struct{}{}
	}
	return nil
}

func protectedImageMountTarget(target string) bool {
	switch target {
	case "/bin", "/dev", "/etc", "/lib", "/lib64", "/mnt", "/proc", "/run", "/sbin", "/sys", "/usr":
		return true
	default:
		return false
	}
}

func pathHasParentReference(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func pathsOverlap(a, b string) bool {
	a = path.Clean(a)
	b = path.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func buildResourceSpec(requestCPUValue, requestMemoryValue, requestEphemeralStorageValue, limitCPUValue, limitMemoryValue, limitEphemeralStorageValue ResourceQuantity) (*commonv1.ResourceSpec, error) {
	requestCPU, err := parseCPUQuantity("request_cpu", requestCPUValue)
	if err != nil {
		return nil, err
	}
	requestMemory, err := parseMemoryQuantity("request_memory", requestMemoryValue)
	if err != nil {
		return nil, err
	}
	requestEphemeralStorage, err := parseMemoryQuantity("request_ephemeral_storage", requestEphemeralStorageValue)
	if err != nil {
		return nil, err
	}
	limitCPU, err := parseCPUQuantity("limit_cpu", limitCPUValue)
	if err != nil {
		return nil, err
	}
	limitMemory, err := parseMemoryQuantity("limit_memory", limitMemoryValue)
	if err != nil {
		return nil, err
	}
	limitEphemeralStorage, err := parseMemoryQuantity("limit_ephemeral_storage", limitEphemeralStorageValue)
	if err != nil {
		return nil, err
	}
	resources := &commonv1.ResourceSpec{}
	if requestCPU > 0 || requestMemory > 0 || requestEphemeralStorage > 0 {
		resources.Requests = &commonv1.ResourceQuantity{
			CpuMilli:              requestCPU,
			MemoryBytes:           requestMemory,
			EphemeralStorageBytes: requestEphemeralStorage,
		}
	}
	if limitCPU > 0 || limitMemory > 0 || limitEphemeralStorage > 0 {
		resources.Limits = &commonv1.ResourceQuantity{
			CpuMilli:              limitCPU,
			MemoryBytes:           limitMemory,
			EphemeralStorageBytes: limitEphemeralStorage,
		}
	}
	if resources.Requests == nil && resources.Limits == nil {
		return nil, nil
	}
	return resources, nil
}

// DeleteService deletes a service by id.
func (c *Client) DeleteService(ctx context.Context, serviceID string) error {
	_, err := c.services.DeleteService(ctx, &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	return mapRPCError(err, "delete service", "")
}

// ListServiceReplicas returns the current replicas for a service.
func (c *Client) ListServiceReplicas(ctx context.Context, serviceID string) ([]*servicev1.ServiceReplica, error) {
	if strings.TrimSpace(serviceID) == "" {
		return nil, requiredError("service_id")
	}
	response, err := c.services.ListServiceReplicas(ctx, &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter: &servicev1.ServiceReplicaListFilter{
			View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
		},
	})
	if err != nil {
		return nil, mapRPCError(err, "list service replicas", "")
	}
	return response.GetReplicas(), nil
}

// CreateTunnelSessionOptions configures a control-plane tunnel session.
type CreateTunnelSessionOptions struct {
	AllocationID string
	LocalTarget  string
	RemotePort   int32
	TTL          time.Duration
	WaitReady    bool
	ReadyTimeout time.Duration
}

// CreateTunnelSessionResult contains the created tunnel session and client token.
type CreateTunnelSessionResult struct {
	Session     *tunnelcontrolv1.TunnelSession
	ClientToken string
}

// CreateTunnelSession creates a tunnel session through the control plane.
func (c *Client) CreateTunnelSession(ctx context.Context, options CreateTunnelSessionOptions) (CreateTunnelSessionResult, error) {
	if options.AllocationID == "" {
		return CreateTunnelSessionResult{}, requiredError("allocation_id")
	}
	if isBlank(options.LocalTarget) {
		return CreateTunnelSessionResult{}, requiredError("local_target")
	}
	if options.RemotePort < 0 {
		return CreateTunnelSessionResult{}, positiveIntError("remote_port")
	}
	if options.TTL < 0 {
		return CreateTunnelSessionResult{}, positiveDurationError("ttl")
	}
	if options.ReadyTimeout < 0 {
		return CreateTunnelSessionResult{}, positiveDurationError("ready_timeout")
	}
	request := &tunnelcontrolv1.CreateTunnelSessionRequest{
		AllocationID: options.AllocationID,
		LocalTarget:  options.LocalTarget,
		WaitReady:    options.WaitReady,
		Ttl:          durationpb.New(options.TTL),
		ReadyTimeout: durationpb.New(options.ReadyTimeout),
	}
	if options.RemotePort > 0 {
		request.RemotePort = &options.RemotePort
	}
	response, err := c.tunnels.CreateTunnelSession(ctx, request)
	if err != nil {
		return CreateTunnelSessionResult{}, mapRPCError(err, "create tunnel session", options.AllocationID)
	}
	return CreateTunnelSessionResult{Session: response.GetSession(), ClientToken: response.GetClientToken()}, nil
}

// GetTunnelSession fetches a tunnel session by id.
func (c *Client) GetTunnelSession(ctx context.Context, sessionID string) (*tunnelcontrolv1.TunnelSession, error) {
	response, err := c.tunnels.GetTunnelSession(ctx, &tunnelcontrolv1.GetTunnelSessionRequest{SessionID: sessionID})
	if err != nil {
		return nil, mapRPCError(err, "get tunnel session", "")
	}
	return response.GetSession(), nil
}

// ListTunnelSessionEvents returns recent tunnel session events.
func (c *Client) ListTunnelSessionEvents(ctx context.Context, sessionID string, limit int32) ([]*tunnelcontrolv1.TunnelSessionEvent, error) {
	response, err := c.tunnels.ListTunnelSessionEvents(ctx, &tunnelcontrolv1.ListTunnelSessionEventsRequest{
		SessionID: sessionID,
		Limit:     limit,
	})
	if err != nil {
		return nil, mapRPCError(err, "list tunnel session events", "")
	}
	return response.GetEvents(), nil
}

// RevokeTunnelSession revokes a tunnel session.
func (c *Client) RevokeTunnelSession(ctx context.Context, sessionID, reason string) (*tunnelcontrolv1.TunnelSession, error) {
	response, err := c.tunnels.RevokeTunnelSession(ctx, &tunnelcontrolv1.RevokeTunnelSessionRequest{
		SessionID: sessionID,
		Reason:    reason,
	})
	if err != nil {
		return nil, mapRPCError(err, "revoke tunnel session", "")
	}
	return response.GetSession(), nil
}

// RenewTunnelSession renews a tunnel session lease.
func (c *Client) RenewTunnelSession(ctx context.Context, sessionID, clientToken string, ttl time.Duration) (*tunnelcontrolv1.TunnelSession, error) {
	response, err := c.tunnels.RenewTunnelSession(ctx, &tunnelcontrolv1.RenewTunnelSessionRequest{
		SessionID:   sessionID,
		ClientToken: clientToken,
		Ttl:         durationpb.New(ttl),
	})
	if err != nil {
		return nil, mapRPCError(err, "renew tunnel session", "")
	}
	return response.GetSession(), nil
}
