package process

import (
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func (r *Registry) Start(request StartRequest) (Status, error) {
	if len(request.Args) == 0 {
		return Status{}, fmt.Errorf("process args must not be empty")
	}
	id := "proc-" + strconv.FormatUint(atomic.AddUint64(&r.nextID, 1), 10)
	now := time.Now().UTC()
	managed := &managedProcess{
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
	managedProxy, managedProxyEnv, err := startManagedProxy(request.ManagedProxy)
	if err != nil {
		return Status{}, err
	}
	managed.managedProxy = managedProxy
	request.Env = withManagedProxyEnv(request.Env, managedProxyEnv)

	cmd := exec.Command(request.Args[0], request.Args[1:]...)
	cmd.Dir = processCwd(request.Cwd, r.cwd, user, hasUser)
	cmd.Env = proc.MergeEnv(proc.MergeEnv(r.env, user.env()), request.Env)
	if request.Terminal {
		request.OpenStdin = true
	} else if request.OpenStdin {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			_ = managed.closeManagedProxy()
			return Status{}, err
		}
		managed.stdin = stdin
	} else if request.Stdin != "" {
		cmd.Stdin = strings.NewReader(request.Stdin)
	}
	if err := managed.configureOutput(cmd, request); err != nil {
		_ = managed.closeManagedProxy()
		return Status{}, err
	}
	if !request.Terminal && !request.CaptureOutput && !request.StreamOutput {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	if err := r.reserve(id, managed); err != nil {
		_ = managed.closeManagedProxy()
		return Status{}, err
	}

	if err := managed.start(cmd, request, user, hasUser); err != nil {
		managedProxyReport := managed.closeManagedProxy()
		finishedAt := time.Now().UTC()
		exitCode := proc.RuntimeStartExitCode
		managed.mu.Lock()
		managed.status.State = ProcessStateFailed
		managed.status.ExitCode = &exitCode
		managed.status.FinishedAt = &finishedAt
		managed.status.LastError = err.Error()
		managed.status.ManagedProxyReport = managedProxyReport
		managed.mu.Unlock()
		if managed.outputs != nil {
			managed.outputs.close()
		}
		close(managed.done)
		r.recordDone(id)
		return managed.snapshot(), nil
	}

	managed.mu.Lock()
	managed.cmd = cmd
	managed.status.State = ProcessStateRunning
	managed.status.PID = cmd.Process.Pid
	managed.mu.Unlock()
	managed.startPipeOutputCopy()

	waitCh := r.waiter.Watch(cmd)
	go func() {
		managed.finishFromWait(<-waitCh)
		r.recordDone(id)
	}()
	if timeout > 0 {
		go managed.killAfter(timeout)
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
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Release()
	}
	p.waitForOutput()
	managedProxyReport := p.closeManagedProxy()
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
	p.status.ManagedProxyReport = managedProxyReport
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

func (p *managedProcess) closeManagedProxy() *ManagedProxyReport {
	p.mu.RLock()
	session := p.managedProxy
	p.mu.RUnlock()
	if session == nil {
		return nil
	}
	report := session.closeAndReport()
	p.mu.Lock()
	if p.managedProxy == session {
		p.managedProxy = nil
	}
	p.mu.Unlock()
	return report
}

func (p *managedProcess) killAfter(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return
	case <-timer.C:
	}
	p.mu.RLock()
	cmd := p.cmd
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	p.mu.Lock()
	if p.status.State != ProcessStateRunning {
		p.mu.Unlock()
		return
	}
	if p.status.LastError == "" {
		p.status.LastError = fmt.Sprintf("process timed out after %s", timeout)
	}
	p.mu.Unlock()
	_ = proc.KillProcessGroup(cmd.Process.Pid)
}
