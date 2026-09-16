package process

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func (r *Registry) Start(request StartRequest) (Status, error) {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	if r.closing {
		return Status{}, fmt.Errorf("process registry is shutting down")
	}
	if len(request.Args) == 0 {
		return Status{}, fmt.Errorf("process args must not be empty")
	}
	id := "proc-" + strconv.FormatUint(atomic.AddUint64(&r.nextID, 1), 10)
	now := time.Now().UTC()
	managed := &managedProcess{
		waiter: r.waiter,
		status: Status{
			ID:        id,
			State:     ProcessStateStarting,
			StartedAt: &now,
		},
		terminal: request.Terminal,
		done:     make(chan struct{}),
	}

	user, hasUser, err := resolveProcessUser(request.User)
	if err != nil {
		return Status{}, err
	}
	timeout, err := processTimeout(request.TimeoutMs)
	if err != nil {
		return Status{}, err
	}
	cmd := exec.Command(request.Args[0], request.Args[1:]...)
	// Check capacity before allocating pipes; admission failure must not leak FDs.
	if err := r.checkCapacity(); err != nil {
		return Status{}, err
	}
	published := false
	defer func() {
		if !published {
			managed.closeIO()
			for _, stream := range []any{cmd.Stdin, cmd.Stdout, cmd.Stderr} {
				if file, ok := stream.(*os.File); ok {
					_ = file.Close()
				}
			}
			r.mu.Lock()
			delete(r.procs, id)
			r.mu.Unlock()
		}
	}()
	cmd.Dir = processCwd(request.Cwd, r.cwd, user, hasUser)
	cmd.Env = proc.MergeEnv(proc.MergeEnv(r.env, user.env()), request.Env)
	if request.Terminal {
		request.OpenStdin = true
	} else if request.OpenStdin || request.Stdin != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return Status{}, err
		}
		managed.stdin = stdin
	}
	if err := managed.configureOutput(cmd, request); err != nil {
		return Status{}, err
	}

	managed.mu.Lock()
	r.mu.Lock()
	r.procs[id] = managed
	r.mu.Unlock()
	waitCh, startErr := r.waiter.Start(cmd, func() error {
		if err := managed.start(cmd, request, user, hasUser); err != nil {
			return err
		}
		managed.cmd = cmd
		managed.status.PID = cmd.Process.Pid
		return nil
	})
	managed.mu.Unlock()
	if startErr != nil {
		finishedAt := time.Now().UTC()
		exitCode := proc.RuntimeStartExitCode
		managed.mu.Lock()
		managed.status.State = ProcessStateFailed
		managed.status.ExitCode = &exitCode
		managed.status.FinishedAt = &finishedAt
		managed.status.LastError = startErr.Error()
		managed.mu.Unlock()
		if managed.outputs != nil {
			managed.outputs.close()
		}
		close(managed.done)
		managed.closeIO()
		published = true
		r.recordDone(id)
		return managed.snapshot(), nil
	}

	managed.mu.Lock()
	managed.status.State = ProcessStateRunning
	managed.mu.Unlock()
	managed.startPipeOutputCopy()

	published = true
	go func() {
		managed.finishFromWait(<-waitCh)
		r.recordDone(id)
	}()
	if timeout > 0 {
		go managed.killAfter(timeout)
	}
	if request.Stdin != "" {
		go func() {
			_ = managed.writeStdin([]byte(request.Stdin))
			if !request.Terminal && !request.OpenStdin {
				_ = managed.closeStdin()
			}
		}()
	}
	return managed.snapshot(), nil
}

func processTimeout(timeoutMs int64) (time.Duration, error) {
	if timeoutMs < 0 {
		return 0, fmt.Errorf("process timeoutMs must be non-negative")
	}
	if timeoutMs > math.MaxInt64/int64(time.Millisecond) {
		return 0, fmt.Errorf("process timeoutMs is too large")
	}
	return time.Duration(timeoutMs) * time.Millisecond, nil
}

func processCwd(requestCwd, baseCwd string, user processUser, hasUser bool) string {
	requestCwd = strings.TrimSpace(requestCwd)
	if requestCwd != "" {
		return requestCwd
	}
	baseCwd = strings.TrimSpace(baseCwd)
	userHome := user.defaultCwd()
	if hasUser && userHome != "" && (baseCwd == "" || baseCwd == "/") {
		return userHome
	}
	if baseCwd != "" {
		return baseCwd
	}
	return userHome
}

func (p *managedProcess) finishFromWait(waitResult proc.Result) {
	p.waitForOutput()
	finishedAt := time.Now().UTC()
	exitCode := waitResult.ExitCode
	signalName := ""
	if waitResult.Signal != nil {
		signalName = waitResult.Signal.String()
	}
	p.mu.Lock()
	p.status.State = ProcessStateExited
	p.status.ExitCode = &exitCode
	p.status.Signal = signalName
	p.status.FinishedAt = &finishedAt
	if waitResult.Err != nil {
		if p.status.LastError == "" {
			p.status.LastError = waitResult.Err.Error()
		} else {
			p.status.LastError = p.status.LastError + ": " + waitResult.Err.Error()
		}
	}
	p.mu.Unlock()
	if p.outputs != nil {
		p.outputs.close()
	}
	close(p.done)
}

func (p *managedProcess) killAfter(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return
	case <-timer.C:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status.State != ProcessStateRunning {
		return
	}
	err := p.waiter.Signal(p.cmd, os.Kill)
	if errors.Is(err, os.ErrProcessDone) {
		return
	}
	if p.status.LastError == "" {
		p.status.LastError = fmt.Sprintf("process timed out after %s", timeout)
	}
	if err != nil {
		p.status.LastError += ": " + err.Error()
	}
}
