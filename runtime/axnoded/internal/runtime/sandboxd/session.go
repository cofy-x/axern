package sandboxd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/execflow"
)

const (
	streamDrainTimeout     = 10 * time.Second
	sessionKillWaitTimeout = time.Second
)

var sessionShutdownTimeout = 2 * time.Second

type SessionClient interface {
	StartProcess(context.Context, ProcessStartRequest) (ProcessStatus, error)
	WriteProcessStdin(context.Context, string, []byte) (ProcessStatus, error)
	CloseProcessStdin(context.Context, string) (ProcessStatus, error)
	ResizeProcess(context.Context, string, uint32, uint32) (ProcessStatus, error)
	SignalProcess(context.Context, string, string) (ProcessStatus, error)
	StreamProcess(context.Context, string, func(ProcessStreamEvent) error) error
	WaitProcess(context.Context, string) (ProcessStatus, error)
}

var NewSessionClient = func(socketPath string) SessionClient {
	return NewClient(socketPath)
}

func OpenExecSession(ctx context.Context, request *apipb.ExecSessionOpen, options contract.HandlerOptions, containerRoot string) (contract.Session, error) {
	socketPath, err := processSocketPath(containerRoot, options, request.GetTty(), request.GetManagedProxy() != nil)
	if err != nil {
		return nil, err
	}
	client := NewSessionClient(socketPath)
	started, err := client.StartProcess(ctx, ProcessStartRequest{
		Args:         request.GetCommand(),
		Cwd:          request.GetCwd(),
		Env:          processEnvList(execflow.KeyValueMap(request.GetEnvs())),
		User:         request.GetUser(),
		OpenStdin:    true,
		StreamOutput: true,
		Terminal:     request.GetTty(),
		InitialCols:  request.GetInitialSize().GetCols(),
		InitialRows:  request.GetInitialSize().GetRows(),
		ManagedProxy: managedProxySpec(request.GetManagedProxy()),
	})
	if err != nil {
		return nil, processOperationError("start exec session", err)
	}
	return NewSession(ctx, client, started.ID), nil
}

type Session struct {
	ctx             context.Context
	cancel          context.CancelFunc
	client          SessionClient
	processID       string
	base            *execflow.SessionState
	closeOnce       sync.Once
	closeStdinOnce  sync.Once
	closeStdinErr   error
	streamDone      chan error
	streamDrainOnce sync.Once
}

func NewSession(ctx context.Context, client SessionClient, processID string) *Session {
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &Session{
		ctx:        sessionCtx,
		cancel:     cancel,
		client:     client,
		processID:  processID,
		base:       execflow.NewSessionState(),
		streamDone: make(chan error, 1),
	}
	go session.streamOutput()
	go session.waitProcess()
	return session
}

func (s *Session) Write(data []byte) error {
	_, err := s.client.WriteProcessStdin(s.ctx, s.processID, data)
	return err
}

func (s *Session) CloseStdin() error {
	s.closeStdinOnce.Do(func() {
		_, s.closeStdinErr = s.client.CloseProcessStdin(s.ctx, s.processID)
	})
	return s.closeStdinErr
}

func (s *Session) Resize(cols uint32, rows uint32) error {
	_, err := s.client.ResizeProcess(s.ctx, s.processID, cols, rows)
	return err
}

func (s *Session) Signal(signal string) error {
	_, err := s.client.SignalProcess(s.ctx, s.processID, signal)
	return err
}

func (s *Session) Recv() (contract.Chunk, error) {
	return s.base.Recv()
}

func (s *Session) Wait() (contract.Exit, error) {
	return s.base.Wait()
}

func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), sessionShutdownTimeout)
		defer cancel()
		s.closeStdinOnce.Do(func() {
			_, s.closeStdinErr = s.client.CloseProcessStdin(cleanupCtx, s.processID)
		})
		err = s.closeStdinErr
		_, _ = s.client.SignalProcess(cleanupCtx, s.processID, "TERM")
		waitDone := make(chan struct{})
		go func() {
			_, _ = s.base.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-cleanupCtx.Done():
			killCtx, killCancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = s.client.SignalProcess(killCtx, s.processID, "KILL")
			killCancel()
			killWait := time.NewTimer(sessionKillWaitTimeout)
			defer killWait.Stop()
			select {
			case <-waitDone:
			case <-killWait.C:
			}
		}
		s.cancel()
	})
	return err
}

func (s *Session) streamOutput() {
	err := s.client.StreamProcess(s.ctx, s.processID, func(event ProcessStreamEvent) error {
		if len(event.Stdout) > 0 {
			s.base.EmitStdout(event.Stdout)
		}
		if len(event.Stderr) > 0 {
			s.base.EmitStderr(event.Stderr)
		}
		return nil
	})
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	if err != nil && s.ctx.Err() != nil {
		err = nil
	}
	s.streamDone <- err
	close(s.streamDone)
}

func (s *Session) waitProcess() {
	status, err := s.client.WaitProcess(s.ctx, s.processID)
	if err != nil {
		s.base.FinishWait(contract.Exit{}, err)
		s.base.FinishOutput()
		return
	}

	if err := s.drainStream(); err != nil {
		s.base.FinishWait(contract.Exit{}, err)
		s.base.FinishOutput()
		return
	}
	exitCode, err := processExitCode(status)
	if err != nil {
		s.base.FinishWait(contract.Exit{}, err)
		s.base.FinishOutput()
		return
	}
	s.base.FinishWait(contract.Exit{
		Timestamp:          time.Now(),
		Status:             exitCode,
		ManagedProxyReport: managedProxyReport(status.ManagedProxyReport),
	}, nil)
	s.base.FinishOutput()
}

func (s *Session) drainStream() error {
	var streamErr error
	s.streamDrainOnce.Do(func() {
		select {
		case streamErr = <-s.streamDone:
		case <-time.After(streamDrainTimeout):
			streamErr = fmt.Errorf("sandboxd process stream did not finish within %s", streamDrainTimeout)
		}
	})
	return streamErr
}
