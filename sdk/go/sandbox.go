package axernsdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

var defaultSandboxArgv = []string{"/bin/sh", "-lc", "sleep infinity"}

// SandboxOptions describes the service-backed sandbox to create or attach to.
type SandboxOptions struct {
	Client                  *Client
	TemplateID              string
	Image                   string
	EnvironmentID           string
	Namespace               string
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
	ReadyTimeout            time.Duration
	Labels                  map[string]string
	RegistryCredentialID    string
	RootFSReadonly          bool
}

// Sandbox is an SDK-owned programmable sandbox backed by an Axern service
// allocation.
type Sandbox struct {
	client             *Client
	options            SandboxOptions
	createdEnvironment bool
	environmentID      string
	serviceID          string
	state              SandboxState
	started            bool
	tunnelsMu          sync.Mutex
	tunnels            map[*SandboxTunnel]struct{}
	processesMu        sync.Mutex
	processes          map[*SandboxProcess]struct{}
}

// SandboxState is the lightweight runtime identity for a started sandbox.
type SandboxState struct {
	EnvironmentID         string
	ServiceID             string
	AllocationID          string
	NodeID                string
	Attempt               int64
	StartedAt             time.Time
	TunnelSessionID       string
	BoundAddr             string
	WorkspacePreparation  *commonv1.WorkspacePreparationFacts
	VerifierMaterializeMs int64
}

// NewSandbox constructs a sandbox handle. Call Start before using runtime APIs.
func NewSandbox(options SandboxOptions) (*Sandbox, error) {
	if err := validateSandboxOptions(options); err != nil {
		return nil, err
	}
	return &Sandbox{client: options.Client, options: options}, nil
}

// Start creates the backing environment/service when needed and waits for a
// ready allocation.
func (s *Sandbox) Start(ctx context.Context) error {
	if s.started {
		return nil
	}
	environmentID := s.options.EnvironmentID
	if environmentID == "" {
		environment, err := s.client.CreateEnvironment(ctx, CreateEnvironmentOptions{
			Namespace:            s.options.Namespace,
			TemplateID:           s.options.TemplateID,
			Image:                s.options.Image,
			RegistryCredentialID: s.options.RegistryCredentialID,
			RootFSReadonly:       s.options.RootFSReadonly,
			Labels:               sandboxLabels(s.options.Labels),
		})
		if err != nil {
			return err
		}
		s.createdEnvironment = true
		s.environmentID = environment.GetID()
		environmentID = s.environmentID
	}
	service, err := s.client.CreateService(ctx, CreateServiceOptions{
		Namespace:               s.options.Namespace,
		EnvironmentID:           environmentID,
		Argv:                    sandboxArgv(s.options.Argv),
		Env:                     s.options.Env,
		Cwd:                     s.options.Cwd,
		RuntimeClass:            s.options.RuntimeClass,
		Volumes:                 s.options.Volumes,
		ImageMounts:             s.options.ImageMounts,
		WorkspaceImage:          s.options.WorkspaceImage,
		RequestCPU:              s.options.RequestCPU,
		RequestMemory:           s.options.RequestMemory,
		RequestEphemeralStorage: s.options.RequestEphemeralStorage,
		LimitCPU:                s.options.LimitCPU,
		LimitMemory:             s.options.LimitMemory,
		LimitEphemeralStorage:   s.options.LimitEphemeralStorage,
		Labels:                  sandboxLabels(s.options.Labels),
	})
	if err != nil {
		_ = s.Close(ctx)
		return err
	}
	s.serviceID = service.GetID()
	replica, err := s.waitReadyReplica(ctx, s.serviceID, defaultDuration(s.options.ReadyTimeout, 3*time.Minute))
	if err != nil {
		_ = s.Close(ctx)
		return err
	}
	s.state = SandboxState{
		EnvironmentID:        environmentID,
		ServiceID:            service.GetID(),
		AllocationID:         replica.GetID(),
		NodeID:               replica.GetNodeID(),
		Attempt:              replica.GetAttempt(),
		StartedAt:            time.Now(),
		WorkspacePreparation: replica.GetWorkspacePreparation(),
	}
	s.started = true
	return nil
}

// Close closes SDK-owned processes and tunnels, then deletes SDK-owned
// service/environment resources.
func (s *Sandbox) Close(ctx context.Context) error {
	var firstErr error
	if err := s.closeProcesses(); err != nil {
		firstErr = err
	}
	if err := s.closeTunnels(ctx); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}
	if s.serviceID != "" {
		if err := s.client.DeleteService(ctx, s.serviceID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
		s.serviceID = ""
	}
	if s.createdEnvironment && s.environmentID != "" {
		if err := s.client.DeleteEnvironment(ctx, s.environmentID); err != nil && firstErr == nil {
			firstErr = err
		}
		s.createdEnvironment = false
		s.environmentID = ""
	}
	s.started = false
	s.state = SandboxState{}
	return firstErr
}

// State returns the current lightweight state for a started sandbox.
func (s *Sandbox) State() (SandboxState, error) {
	if !s.started {
		return SandboxState{}, ErrSandboxNotStarted
	}
	return s.state, nil
}

// Exec runs a command in the sandbox and collects stdout/stderr.
func (s *Sandbox) Exec(ctx context.Context, command any, options ExecOptions) (ExecResult, error) {
	node, err := s.nodeClient()
	if err != nil {
		return ExecResult{}, err
	}
	return node.Exec(ctx, command, options)
}

func (s *Sandbox) nodeClient() (*NodeSandboxClient, error) {
	if !s.started {
		return nil, ErrSandboxNotStarted
	}
	return s.client.NodeSandbox(s.state.AllocationID)
}

func (s *Sandbox) registerTunnel(tunnel *SandboxTunnel) {
	s.tunnelsMu.Lock()
	defer s.tunnelsMu.Unlock()
	if s.tunnels == nil {
		s.tunnels = map[*SandboxTunnel]struct{}{}
	}
	s.tunnels[tunnel] = struct{}{}
}

func (s *Sandbox) unregisterTunnel(tunnel *SandboxTunnel) {
	s.tunnelsMu.Lock()
	defer s.tunnelsMu.Unlock()
	delete(s.tunnels, tunnel)
}

func (s *Sandbox) registerProcess(process *SandboxProcess) {
	s.processesMu.Lock()
	defer s.processesMu.Unlock()
	if s.processes == nil {
		s.processes = map[*SandboxProcess]struct{}{}
	}
	s.processes[process] = struct{}{}
}

func (s *Sandbox) unregisterProcess(process *SandboxProcess) {
	s.processesMu.Lock()
	defer s.processesMu.Unlock()
	delete(s.processes, process)
}

func (s *Sandbox) closeProcesses() error {
	s.processesMu.Lock()
	processes := make([]*SandboxProcess, 0, len(s.processes))
	for process := range s.processes {
		processes = append(processes, process)
	}
	s.processesMu.Unlock()

	var firstErr error
	for _, process := range processes {
		if err := process.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Sandbox) closeTunnels(ctx context.Context) error {
	s.tunnelsMu.Lock()
	tunnels := make([]*SandboxTunnel, 0, len(s.tunnels))
	for tunnel := range s.tunnels {
		tunnels = append(tunnels, tunnel)
	}
	s.tunnelsMu.Unlock()

	var firstErr error
	for _, tunnel := range tunnels {
		if err := tunnel.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Sandbox) waitReadyReplica(ctx context.Context, serviceID string, timeout time.Duration) (*servicev1.ServiceReplica, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	watch, err := s.client.WatchService(waitCtx, serviceID, 0)
	if err != nil {
		return nil, err
	}
	defer watch.Close()
	var last []*servicev1.ServiceReplica
	for {
		service, err := watch.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil, readyReplicaTimeoutError(serviceID, timeout, last)
			}
			return nil, err
		}
		replicas, err := s.client.ListServiceReplicas(waitCtx, serviceID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil, readyReplicaTimeoutError(serviceID, timeout, last)
			}
			return nil, err
		}
		last = replicas
		for _, replica := range replicas {
			if replica.GetReady() &&
				!replica.GetEnded() &&
				!replica.GetOutdated() &&
				replica.GetStatus() == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
				return replica, nil
			}
		}
		switch service.GetStatus() {
		case servicev1.ServiceStatus_SERVICE_STATUS_FAILED,
			servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
			servicev1.ServiceStatus_SERVICE_STATUS_DELETED:
			return nil, fmt.Errorf("service %s became %s before a sandbox replica was ready: %s", serviceID, service.GetStatus(), service.GetMessage())
		}
	}
}

func readyReplicaTimeoutError(serviceID string, timeout time.Duration, replicas []*servicev1.ServiceReplica) error {
	return fmt.Errorf("service %s did not produce a ready sandbox replica within %s: %s", serviceID, timeout, replicaDetails(replicas))
}

func sandboxLabels(labels map[string]string) map[string]string {
	values := cloneMap(labels)
	if values == nil {
		values = map[string]string{}
	}
	values["axern.sdk.resource"] = "sandbox"
	return values
}

func sandboxArgv(argv []string) []string {
	if len(argv) == 0 {
		return append([]string(nil), defaultSandboxArgv...)
	}
	return append([]string(nil), argv...)
}

func validateSandboxOptions(options SandboxOptions) error {
	if options.Client == nil {
		return requiredError("client")
	}
	sourceCount := countNonEmpty(options.TemplateID, options.Image, options.EnvironmentID)
	if sourceCount != 1 {
		return ErrInvalidSource
	}
	if options.ReadyTimeout < 0 {
		return positiveDurationError("ready_timeout")
	}
	if _, err := parseCPUQuantity("request_cpu", options.RequestCPU); err != nil {
		return err
	}
	if _, err := parseMemoryQuantity("request_memory", options.RequestMemory); err != nil {
		return err
	}
	if _, err := parseMemoryQuantity("request_ephemeral_storage", options.RequestEphemeralStorage); err != nil {
		return err
	}
	if _, err := parseCPUQuantity("limit_cpu", options.LimitCPU); err != nil {
		return err
	}
	if _, err := parseMemoryQuantity("limit_memory", options.LimitMemory); err != nil {
		return err
	}
	if _, err := parseMemoryQuantity("limit_ephemeral_storage", options.LimitEphemeralStorage); err != nil {
		return err
	}
	if err := validateImageMounts(options.ImageMounts); err != nil {
		return err
	}
	if err := validateWorkspaceImage(options.WorkspaceImage); err != nil {
		return err
	}
	if err := validateWorkspaceImageMounts(options.WorkspaceImage, options.ImageMounts, options.Volumes); err != nil {
		return err
	}
	return nil
}

func replicaDetails(replicas []*servicev1.ServiceReplica) string {
	if len(replicas) == 0 {
		return "no replicas"
	}
	details := ""
	for index, replica := range replicas {
		if index > 0 {
			details += ", "
		}
		details += fmt.Sprintf("%s:%s:%s", replica.GetID(), replica.GetStatus(), replica.GetMessage())
	}
	return details
}
