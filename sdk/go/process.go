package axernsdk

import (
	"context"
	"errors"
	"io"
	"iter"
	"sync"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
)

// ProcessOptions configures an attached sandbox process.
type ProcessOptions struct {
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	ManagedProxy *ManagedProxyOptions
}

// ProcessEventKind identifies a process stream event.
type ProcessEventKind string

const (
	ProcessEventStdout ProcessEventKind = "stdout"
	ProcessEventStderr ProcessEventKind = "stderr"
	ProcessEventExit   ProcessEventKind = "exit"
)

// ProcessEvent is a stdout, stderr, or exit event from an attached process.
type ProcessEvent struct {
	Kind               ProcessEventKind
	Data               []byte
	ExitCode           int32
	Message            string
	ManagedProxyReport *ManagedProxyReport
}

// ProcessResult is the exit status for a sandbox process.
type ProcessResult struct {
	ExitCode           int32
	Message            string
	ManagedProxyReport *ManagedProxyReport
}

// ProcessOutput contains collected stdout, stderr, and exit status.
type ProcessOutput struct {
	ProcessResult
	Stdout []byte
	Stderr []byte
}

// SandboxProcess is an attached sandbox process with stream controls.
type SandboxProcess struct {
	allocationID string
	process      sandboxProcessClient
	sandbox      *Sandbox
	mu           sync.Mutex
	exit         *ProcessResult
	closeOnce    sync.Once
	closeErr     error
}

type sandboxProcessClient interface {
	Write([]byte) error
	CloseStdin() error
	Resize(uint32, uint32) error
	Signal(string) error
	Recv() (nodeclient.ProcessEvent, error)
	Close() error
}

// Process starts an attached process in the sandbox.
func (s *Sandbox) Process(ctx context.Context, command any, options ProcessOptions) (*SandboxProcess, error) {
	node, err := s.nodeClient()
	if err != nil {
		return nil, err
	}
	process, err := node.Process(ctx, command, options)
	if err != nil {
		return nil, err
	}
	process.sandbox = s
	s.registerProcess(process)
	return process, nil
}

// Process starts an attached process in the allocation.
func (n *NodeSandboxClient) Process(ctx context.Context, command any, options ProcessOptions) (*SandboxProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	if err := validateProcessOptions(options); err != nil {
		return nil, err
	}
	argv, err := normalizeCommand(command)
	if err != nil {
		return nil, err
	}
	process, err := n.rpcClient().Process(ctx, argv, nodeclient.Options{
		Env:          options.Env,
		Cwd:          options.Cwd,
		Timeout:      options.Timeout,
		User:         options.User,
		TTY:          options.TTY,
		ManagedProxy: nodeManagedProxyOptions(options.ManagedProxy),
	})
	if err != nil {
		return nil, mapRPCError(err, "sandbox process", n.allocationID)
	}
	return &SandboxProcess{allocationID: n.allocationID, process: process}, nil
}

// Write sends bytes to the process stdin stream.
func (p *SandboxProcess) Write(data []byte) error {
	if p == nil || p.process == nil {
		return ErrProcessClosed
	}
	return mapProcessError(p.process.Write(data), "sandbox process write", p.allocationID)
}

// WriteString sends a string to the process stdin stream.
func (p *SandboxProcess) WriteString(data string) error {
	return p.Write([]byte(data))
}

// CloseStdin closes the process stdin stream.
func (p *SandboxProcess) CloseStdin() error {
	if p == nil || p.process == nil {
		return ErrProcessClosed
	}
	return mapProcessError(p.process.CloseStdin(), "sandbox process close stdin", p.allocationID)
}

// Resize sends a terminal resize event for TTY processes.
func (p *SandboxProcess) Resize(cols, rows uint32) error {
	if p == nil || p.process == nil {
		return ErrProcessClosed
	}
	if cols == 0 {
		return validationError("cols", "must be greater than zero")
	}
	if rows == 0 {
		return validationError("rows", "must be greater than zero")
	}
	return mapProcessError(p.process.Resize(cols, rows), "sandbox process resize", p.allocationID)
}

// Signal sends a named signal to the process.
func (p *SandboxProcess) Signal(signal string) error {
	if p == nil || p.process == nil {
		return ErrProcessClosed
	}
	if signal == "" {
		return requiredError("signal")
	}
	return mapProcessError(p.process.Signal(signal), "sandbox process signal", p.allocationID)
}

// Terminate sends SIGTERM to the process.
func (p *SandboxProcess) Terminate() error {
	return p.Signal("TERM")
}

// Kill sends SIGKILL to the process.
func (p *SandboxProcess) Kill() error {
	return p.Signal("KILL")
}

// Recv receives the next process event.
func (p *SandboxProcess) Recv() (ProcessEvent, error) {
	if p == nil || p.process == nil {
		return ProcessEvent{}, ErrProcessClosed
	}
	event, err := p.process.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return ProcessEvent{}, err
		}
		return ProcessEvent{}, mapProcessError(err, "sandbox process recv", p.allocationID)
	}
	sdkEvent := ProcessEvent{
		Kind:               ProcessEventKind(event.Kind),
		Data:               event.Data,
		ExitCode:           event.ExitCode,
		Message:            event.Message,
		ManagedProxyReport: sdkManagedProxyReport(event.ManagedProxyReport),
	}
	if sdkEvent.Kind == ProcessEventExit {
		p.rememberExit(ProcessResult{
			ExitCode:           sdkEvent.ExitCode,
			Message:            sdkEvent.Message,
			ManagedProxyReport: sdkEvent.ManagedProxyReport,
		})
		_ = p.closeProcess()
	}
	return sdkEvent, nil
}

// Events returns an iterator over process events until exit or error.
func (p *SandboxProcess) Events() iter.Seq2[ProcessEvent, error] {
	return func(yield func(ProcessEvent, error) bool) {
		for {
			event, err := p.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(ProcessEvent{}, err)
				}
				return
			}
			if !yield(event, nil) || event.Kind == ProcessEventExit {
				return
			}
		}
	}
}

// Wait drains events until the process exit status is observed.
func (p *SandboxProcess) Wait() (ProcessResult, error) {
	if p == nil || p.process == nil {
		return ProcessResult{}, ErrProcessClosed
	}
	p.mu.Lock()
	if p.exit != nil {
		result := *p.exit
		p.mu.Unlock()
		return result, nil
	}
	p.mu.Unlock()

	for {
		event, err := p.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = p.closeProcess()
				return ProcessResult{}, ErrProcessExitMissing
			}
			return ProcessResult{}, err
		}
		if event.Kind == ProcessEventExit {
			return ProcessResult{
				ExitCode:           event.ExitCode,
				Message:            event.Message,
				ManagedProxyReport: event.ManagedProxyReport,
			}, nil
		}
	}
}

// Output collects stdout, stderr, and exit status.
func (p *SandboxProcess) Output() (ProcessOutput, error) {
	var output ProcessOutput
	for event, err := range p.Events() {
		if err != nil {
			return output, err
		}
		switch event.Kind {
		case ProcessEventStdout:
			output.Stdout = append(output.Stdout, event.Data...)
		case ProcessEventStderr:
			output.Stderr = append(output.Stderr, event.Data...)
		case ProcessEventExit:
			output.ProcessResult = ProcessResult{
				ExitCode:           event.ExitCode,
				Message:            event.Message,
				ManagedProxyReport: event.ManagedProxyReport,
			}
			return output, nil
		}
	}
	return output, ErrProcessExitMissing
}

// Close closes the attached process stream.
func (p *SandboxProcess) Close() error {
	if p == nil || p.process == nil {
		return nil
	}
	return p.closeProcess()
}

func (p *SandboxProcess) rememberExit(result ProcessResult) {
	p.mu.Lock()
	if p.exit == nil {
		p.exit = &result
	}
	p.mu.Unlock()
}

func (p *SandboxProcess) closeProcess() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.process.Close()
		if p.sandbox != nil {
			p.sandbox.unregisterProcess(p)
		}
	})
	return p.closeErr
}

func mapProcessError(err error, operation, allocationID string) error {
	if errors.Is(err, nodeclient.ErrProcessClosed) {
		return ErrProcessClosed
	}
	return mapRPCError(err, operation, allocationID)
}

func validateProcessOptions(options ProcessOptions) error {
	if options.Timeout < 0 {
		return positiveDurationError("timeout")
	}
	return nil
}
