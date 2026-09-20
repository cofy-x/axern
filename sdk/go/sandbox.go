package axernsdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

var defaultSandboxArgv = []string{"/bin/sh", "-lc", "sleep infinity"}

// SandboxOptions describes the allocation-backed sandbox to create or attach to.
type SandboxOptions struct {
	Client                  *Client
	Image                   string
	EnvironmentID           string
	Namespace               string
	Argv                    []string
	Env                     map[string]string
	Cwd                     string
	NetworkPolicy           *NetworkPolicy
	ExtensionCapabilities   []ExtensionCapability
	DeclaredOutputs         []DeclaredOutput
	ImageMounts             []ImageMount
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

// Sandbox is an SDK-owned programmable sandbox backed by an Axern run
// allocation.
type Sandbox struct {
	client             *Client
	options            SandboxOptions
	createdEnvironment bool
	environmentID      string
	runID              string
	state              SandboxState
	started            bool
	tunnelsMu          sync.Mutex
	tunnels            map[*SandboxTunnel]struct{}
	processesMu        sync.Mutex
	processes          map[*SandboxProcess]struct{}
}

// SandboxState is the lightweight runtime identity for a started sandbox.
type SandboxState struct {
	EnvironmentID   string
	RunID           string
	AllocationID    string
	StartedAt       time.Time
	TunnelSessionID string
	BoundAddr       string
}

// NewSandbox constructs a sandbox handle. Call Start before using runtime APIs.
func NewSandbox(options SandboxOptions) (*Sandbox, error) {
	if err := validateSandboxOptions(options); err != nil {
		return nil, err
	}
	return &Sandbox{client: options.Client, options: options}, nil
}

// Start creates the backing environment/run when needed and waits for a
// running allocation.
func (s *Sandbox) Start(ctx context.Context) error {
	if s.started {
		return nil
	}
	environmentID := s.options.EnvironmentID
	if environmentID == "" {
		environment, err := s.client.CreateEnvironment(ctx, CreateEnvironmentOptions{
			Namespace:            s.options.Namespace,
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
	run, err := s.client.CreateRun(ctx, CreateRunOptions{
		Namespace:               s.options.Namespace,
		EnvironmentID:           environmentID,
		Argv:                    sandboxArgv(s.options.Argv),
		Env:                     s.options.Env,
		Cwd:                     s.options.Cwd,
		NetworkPolicy:           s.options.NetworkPolicy,
		ExtensionCapabilities:   append([]ExtensionCapability(nil), s.options.ExtensionCapabilities...),
		DeclaredOutputs:         append([]DeclaredOutput(nil), s.options.DeclaredOutputs...),
		ImageMounts:             s.options.ImageMounts,
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
	s.runID = run.GetID()
	run, err = s.waitRunning(ctx, s.runID, defaultDuration(s.options.ReadyTimeout, 3*time.Minute))
	if err != nil {
		_ = s.Close(ctx)
		return err
	}
	s.state = SandboxState{
		EnvironmentID: environmentID,
		RunID:         run.GetID(),
		AllocationID:  run.GetAllocationID(),
		StartedAt:     time.Now(),
	}
	s.started = true
	return nil
}

// Close closes SDK-owned processes and tunnels, then deletes SDK-owned
// run/environment resources.
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
	if s.runID != "" {
		if err := s.client.CancelRun(ctx, s.runID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
		s.runID = ""
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

func (s *Sandbox) nodeClient() (*AllocationClient, error) {
	if !s.started {
		return nil, ErrSandboxNotStarted
	}
	return s.client.Allocation(s.state.AllocationID)
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

func (s *Sandbox) waitRunning(ctx context.Context, runID string, timeout time.Duration) (*runv1.Run, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	watch, err := s.client.WatchRun(waitCtx, runID, 0)
	if err != nil {
		return nil, err
	}
	var last *runv1.Run
	for {
		run, err := watch.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil, sandboxRunTimeoutError(runID, timeout, last)
			}
			return nil, err
		}
		last = run
		if run.GetStatus() == runv1.RunStatus_RUN_STATUS_RUNNING && run.GetAllocationID() != "" {
			return run, nil
		}
		switch run.GetStatus() {
		case runv1.RunStatus_RUN_STATUS_SUCCEEDED,
			runv1.RunStatus_RUN_STATUS_FAILED,
			runv1.RunStatus_RUN_STATUS_CANCELLED:
			return nil, fmt.Errorf("run %s became %s before its sandbox allocation was running: %s", runID, run.GetStatus(), run.GetMessage())
		}
	}
}

func sandboxRunTimeoutError(runID string, timeout time.Duration, run *runv1.Run) error {
	detail := "no state observed"
	if run != nil {
		detail = fmt.Sprintf("%s: %s", run.GetStatus(), run.GetMessage())
	}
	return fmt.Errorf("run %s did not reach a running sandbox allocation within %s: %s", runID, timeout, detail)
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
	sourceCount := countNonEmpty(options.Image, options.EnvironmentID)
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
	return nil
}
