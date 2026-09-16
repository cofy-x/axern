package workload

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

type Entrypoint struct {
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
	Env  []string `json:"env"`
}

type ProcessResult struct {
	ExitCode int
	Signal   os.Signal
	Err      error
}

type Supervisor struct {
	entrypoint      Entrypoint
	shutdownTimeout time.Duration
	state           *State
	waiter          *proc.Waiter
	stdout          io.Writer
	stderr          io.Writer

	startMu  sync.Mutex
	started  bool
	closing  bool
	cmd      *exec.Cmd
	done     chan ProcessResult
	finished chan struct{}
	result   ProcessResult
	doneOnce sync.Once
}

func NewSupervisor(entrypoint Entrypoint, shutdownTimeout time.Duration, state *State, waiter *proc.Waiter, stdout io.Writer, stderr io.Writer) *Supervisor {
	return &Supervisor{
		entrypoint:      entrypoint,
		shutdownTimeout: shutdownTimeout,
		state:           state,
		waiter:          waiter,
		stdout:          stdout,
		stderr:          stderr,
		done:            make(chan ProcessResult, 1),
		finished:        make(chan struct{}),
	}
}

func (s *Supervisor) Start() <-chan ProcessResult {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.closing {
		return nil
	}
	if s.started {
		return s.done
	}
	s.started = true
	if len(s.entrypoint.Args) == 0 {
		return nil
	}

	now := time.Now().UTC()
	s.state.SetUserProcess(UserProcessStatus{
		State:     UserStateStarting,
		StartedAt: &now,
	})

	cmd := exec.Command(s.entrypoint.Args[0], s.entrypoint.Args[1:]...)
	if s.entrypoint.Cwd != "" {
		cmd.Dir = s.entrypoint.Cwd
	}
	cmd.Env = proc.MergeEnv(os.Environ(), s.entrypoint.Env)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	cmd.SysProcAttr = proc.SysProcAttr()

	pid := 0
	waitCh, err := s.waiter.Start(cmd, func() error {
		if err := cmd.Start(); err != nil {
			return err
		}
		pid = cmd.Process.Pid
		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintf(s.stderr, "start user process: %v\n", err)
		finishedAt := time.Now().UTC()
		s.state.SetUserProcess(UserProcessStatus{
			State:      UserStateFailed,
			StartedAt:  &now,
			FinishedAt: &finishedAt,
			LastError:  err.Error(),
		})
		s.finish(ProcessResult{ExitCode: proc.RuntimeStartExitCode, Err: err})
		return s.done
	}

	s.cmd = cmd
	s.state.UpdateUserProcess(func(status *UserProcessStatus) {
		status.State = UserStateRunning
		status.PID = pid
	})

	go func() {
		waitResult := <-waitCh
		finishedAt := time.Now().UTC()
		exitCode := waitResult.ExitCode
		signalName := ""
		if waitResult.Signal != nil {
			signalName = waitResult.Signal.String()
		}
		s.state.UpdateUserProcess(func(status *UserProcessStatus) {
			status.State = UserStateExited
			status.ExitCode = &exitCode
			status.Signal = signalName
			status.FinishedAt = &finishedAt
			if waitResult.Err != nil {
				status.LastError = waitResult.Err.Error()
			}
		})
		s.finish(ProcessResult{ExitCode: exitCode, Signal: waitResult.Signal, Err: waitResult.Err})
	}()
	return s.done
}

func (s *Supervisor) Shutdown(signal os.Signal) ProcessResult {
	s.startMu.Lock()
	s.closing = true
	cmd := s.cmd
	s.startMu.Unlock()
	if cmd == nil {
		s.finish(ProcessResult{ExitCode: 0})
		<-s.finished
		return s.result
	}

	s.state.UpdateUserProcess(func(status *UserProcessStatus) {
		if status.State == UserStateRunning {
			status.State = UserStateStopping
		}
	})
	if err := s.waiter.Signal(cmd, signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return ProcessResult{ExitCode: proc.RuntimeStartExitCode, Err: err}
	}

	timer := time.NewTimer(s.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-s.finished:
		return s.result
	case <-timer.C:
		if err := s.waiter.Signal(cmd, os.Kill); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return ProcessResult{ExitCode: proc.RuntimeStartExitCode, Err: err}
		}
		select {
		case <-s.finished:
			return s.result
		case <-time.After(time.Second):
			return ProcessResult{ExitCode: proc.RuntimeStartExitCode, Err: fmt.Errorf("entrypoint kill did not complete")}
		}
	}
}

func (s *Supervisor) finish(result ProcessResult) {
	s.doneOnce.Do(func() {
		s.result = result
		s.done <- result
		close(s.done)
		close(s.finished)
	})
}
