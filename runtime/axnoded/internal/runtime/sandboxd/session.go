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
	socketPath, err := processSocketPath(containerRoot, options)
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
	closeErr        error
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
	s.closeOnce.Do(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), sessionShutdownTimeout)
		defer cancel()
		s.closeStdinOnce.Do(func() {
			_, s.closeStdinErr = s.client.CloseProcessStdin(cleanupCtx, s.processID)
		})
		defer s.cancel()
		_, termErr := s.client.SignalProcess(cleanupCtx, s.processID, "TERM")
		// An access-side Wait can finish because its context was canceled. Only
		// a fresh, independently bounded runtime wait confirms process exit.
		waitErr := s.waitForCleanup(cleanupCtx)
		if waitErr == nil {
			s.closeErr = errors.Join(s.closeStdinErr, termErr)
			return
		}
		killCtx, killCancel := context.WithTimeout(context.Background(), sessionKillWaitTimeout)
		defer killCancel()
		_, killErr := s.client.SignalProcess(killCtx, s.processID, "KILL")
		finalErr := s.waitForCleanup(killCtx)
		if finalErr != nil {
			finalErr = fmt.Errorf("confirm exec session cleanup: %w", errors.Join(waitErr, finalErr))
		}
		s.closeErr = errors.Join(s.closeStdinErr, termErr, killErr, finalErr)
	})
	return s.closeErr
}

func (s *Session) waitForCleanup(ctx context.Context) error {
	status, err := s.client.WaitProcess(ctx, s.processID)
	if err != nil {
		return err
	}
	_, err = processExitCode(status)
	return err
}

func (s *Session) streamOutput() {
	err := s.client.StreamProcess(s.ctx, s.processID, func(event ProcessStreamEvent) error {
		return s.base.EmitContext(s.ctx, contract.Chunk{Stdout: event.Stdout, Stderr: event.Stderr})
	})
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	if err != nil && s.ctx.Err() != nil {
		err = nil
	}
	s.streamDone <- err
	close(s.streamDone)
	if err != nil {
		s.cancel()
	}
}

func (s *Session) waitProcess() {
	status, err := s.client.WaitProcess(s.ctx, s.processID)
	if err != nil {
		s.cancel()
		err = errors.Join(err, <-s.streamDone)
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
		Timestamp: time.Now(),
		Status:    exitCode,
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
			s.cancel()
			<-s.streamDone
		}
	})
	return streamErr
}
