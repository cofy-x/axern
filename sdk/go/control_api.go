package axernsdk

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

type ListEnvironmentsOptions struct {
	Namespace string
	Labels    map[string]string
	Cursor    string
	PageSize  int32
}

type ListRunsOptions struct {
	Namespace string
	Statuses  []runv1.RunStatus
	Labels    map[string]string
	Cursor    string
	PageSize  int32
}

// CreateEnvironmentOptions configures a control-plane environment.
type CreateEnvironmentOptions struct {
	Namespace            string
	TemplateID           string
	Image                string
	RegistryCredentialID string
	RootFSReadonly       bool
	Labels               map[string]string
}

// CreateRunOptions configures a single allocation-backed execution.
type CreateRunOptions struct {
	Namespace               string
	EnvironmentID           string
	Argv                    []string
	Env                     map[string]string
	Cwd                     string
	NetworkPolicy           *NetworkPolicy
	ExtensionCapabilities   []ExtensionCapability
	ImageMounts             []ImageMount
	RequestCPU              ResourceQuantity
	RequestMemory           ResourceQuantity
	RequestEphemeralStorage ResourceQuantity
	LimitCPU                ResourceQuantity
	LimitMemory             ResourceQuantity
	LimitEphemeralStorage   ResourceQuantity
	DeclaredOutputs         []DeclaredOutput
	Labels                  map[string]string
}

type DeclaredOutputFormat string

const (
	DeclaredOutputFile DeclaredOutputFormat = "file"
	DeclaredOutputTar  DeclaredOutputFormat = "tar"
)

// DeclaredOutput asks Axern to seal one bounded file or directory archive
// before the Allocation filesystem is removed.
type DeclaredOutput struct {
	Path      string
	Format    DeclaredOutputFormat
	MediaType string
}

// CreateRun creates a single Axern allocation.
func (c *Client) CreateRun(ctx context.Context, options CreateRunOptions) (*runv1.Run, error) {
	if options.EnvironmentID == "" {
		return nil, requiredError("environment_id")
	}
	if err := validateImageMounts(options.ImageMounts); err != nil {
		return nil, err
	}
	resources, err := buildResourceSpec(options.RequestCPU, options.RequestMemory, options.RequestEphemeralStorage, options.LimitCPU, options.LimitMemory, options.LimitEphemeralStorage)
	if err != nil {
		return nil, err
	}
	response, err := c.runs.CreateRun(ctx, &runv1.CreateRunRequest{
		Namespace:     defaultString(options.Namespace, "default"),
		EnvironmentID: options.EnvironmentID,
		Config: &commonv1.ExecutionConfig{
			Argv:                            append([]string(nil), options.Argv...),
			Env:                             cloneMap(options.Env),
			Cwd:                             options.Cwd,
			Network:                         networkSpec(options.NetworkPolicy),
			ExtensionCapabilityRequirements: extensionCapabilityRequirements(options.ExtensionCapabilities),
			ImageMounts:                     executionImageMounts(options.ImageMounts),
			Resources:                       resources,
			DeclaredOutputs:                 declaredOutputProtos(options.DeclaredOutputs),
		},
		Labels: cloneMap(options.Labels),
	})
	if err != nil {
		return nil, mapRPCError(err, "create run", "")
	}
	return response.GetRun(), nil
}

func declaredOutputProtos(outputs []DeclaredOutput) []*commonv1.DeclaredOutput {
	result := make([]*commonv1.DeclaredOutput, 0, len(outputs))
	for _, output := range outputs {
		format := commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_UNSPECIFIED
		switch output.Format {
		case DeclaredOutputFile:
			format = commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE
		case DeclaredOutputTar:
			format = commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR
		}
		result = append(result, &commonv1.DeclaredOutput{Path: output.Path, Format: format, MediaType: output.MediaType})
	}
	return result
}

// CancelRun releases the allocation owned by runID.
func (c *Client) CancelRun(ctx context.Context, runID string) error {
	if runID == "" {
		return requiredError("run_id")
	}
	_, err := c.runs.CancelRun(ctx, &runv1.CancelRunRequest{RunID: runID})
	return mapRPCError(err, "cancel run", runID)
}

func (c *Client) GetRun(ctx context.Context, runID string) (*runv1.Run, error) {
	if runID == "" {
		return nil, requiredError("run_id")
	}
	response, err := c.runs.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
	if err != nil {
		return nil, mapRPCError(err, "get run", runID)
	}
	return response.GetRun(), nil
}

func (c *Client) ListRuns(ctx context.Context, options ListRunsOptions) (*runv1.ListRunsResponse, error) {
	response, err := c.runs.ListRuns(ctx, &runv1.ListRunsRequest{Filter: &runv1.RunListFilter{
		Namespace: options.Namespace,
		Statuses:  append([]runv1.RunStatus(nil), options.Statuses...),
		Labels:    cloneMap(options.Labels),
		Cursor:    options.Cursor,
		PageSize:  options.PageSize,
	}})
	if err != nil {
		return nil, mapRPCError(err, "list runs", "")
	}
	return response, nil
}

func (c *Client) WaitRun(ctx context.Context, runID string) (*runv1.Run, error) {
	run, err := c.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if runTerminal(run) {
		return run, nil
	}
	watch, err := c.WatchRun(ctx, runID, run.GetVersion())
	if err != nil {
		return nil, err
	}
	for {
		run, err = watch.Recv()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("run %s watch ended before a terminal state", runID)
			}
			return nil, err
		}
		if runTerminal(run) {
			return run, nil
		}
	}
}

func runTerminal(run *runv1.Run) bool {
	switch run.GetStatus() {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED, runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
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

func (c *Client) GetEnvironment(ctx context.Context, environmentID string) (*environmentv1.Environment, error) {
	if environmentID == "" {
		return nil, requiredError("environment_id")
	}
	response, err := c.environments.GetEnvironment(ctx, &environmentv1.GetEnvironmentRequest{EnvironmentID: environmentID})
	if err != nil {
		return nil, mapRPCError(err, "get environment", environmentID)
	}
	return response.GetEnvironment(), nil
}

func (c *Client) ListEnvironments(ctx context.Context, options ListEnvironmentsOptions) (*environmentv1.ListEnvironmentsResponse, error) {
	response, err := c.environments.ListEnvironments(ctx, &environmentv1.ListEnvironmentsRequest{Filter: &environmentv1.ListFilter{
		Namespace: options.Namespace,
		Labels:    cloneMap(options.Labels),
		Cursor:    options.Cursor,
		PageSize:  options.PageSize,
	}})
	if err != nil {
		return nil, mapRPCError(err, "list environments", "")
	}
	return response, nil
}

func networkSpec(policy *NetworkPolicy) *commonv1.NetworkSpec {
	if policy == nil {
		return nil
	}
	return &commonv1.NetworkSpec{EgressPolicy: policy.proto()}
}

// ExtensionCapability is an exact-match, DNS-qualified node extension fact.
// Platform capabilities are inferred by Axern and cannot be requested here.
type ExtensionCapability struct {
	Name  string
	Value string
}

func extensionCapabilityRequirements(values []ExtensionCapability) []*capabilityv1.ExtensionCapabilityRequirement {
	result := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(values))
	for _, value := range values {
		result = append(result, &capabilityv1.ExtensionCapabilityRequirement{Capability: &capabilityv1.ExtensionCapability{Name: strings.TrimSpace(value.Name), Value: value.Value}})
	}
	return result
}

// ImageMount describes a read-only OCI image mounted into the workload rootfs.
type ImageMount struct {
	Image    string
	Target   string
	Readonly bool
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

// CreateTunnelSessionOptions configures a control-plane tunnel session.
type CreateTunnelSessionOptions struct {
	AllocationID string
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
