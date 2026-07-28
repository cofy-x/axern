package workload

import (
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

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan ProcessResult
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
	}
}

func (s *Supervisor) Start() <-chan ProcessResult {
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

	if err := cmd.Start(); err != nil {
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

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	s.state.UpdateUserProcess(func(status *UserProcessStatus) {
		status.State = UserStateRunning
		status.PID = cmd.Process.Pid
	})

	waitCh := s.waiter.Watch(cmd)
	go func() {
		waitResult := <-waitCh
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}
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
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		s.finish(ProcessResult{ExitCode: 0})
		return <-s.done
	}

	s.state.UpdateUserProcess(func(status *UserProcessStatus) {
		if status.State == UserStateRunning {
			status.State = UserStateStopping
		}
	})
	_ = proc.SignalProcessGroup(cmd.Process.Pid, signal)

	timer := time.NewTimer(s.shutdownTimeout)
	defer timer.Stop()
	select {
	case result := <-s.done:
		return result
	case <-timer.C:
		_ = proc.KillProcessGroup(cmd.Process.Pid)
		result := <-s.done
		return result
	}
}

func (s *Supervisor) finish(result ProcessResult) {
	s.doneOnce.Do(func() {
		s.done <- result
		close(s.done)
	})
}
