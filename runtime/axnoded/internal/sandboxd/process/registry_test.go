package process

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestManagedProxySessionRecordsAndInjectsAuth(t *testing.T) {
	var upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	session, env, err := startManagedProxy(&ManagedProxySpec{
		Provider:            "openai",
		UpstreamBaseURL:     upstream.URL + "/v1",
		UpstreamBearerToken: "real-token",
	})
	if err != nil {
		t.Fatalf("startManagedProxy() error = %v", err)
	}
	defer session.closeAndReport()
	if strings.Contains(strings.Join(env, "\n"), "real-token") {
		t.Fatalf("managed proxy env leaked upstream token: %#v", env)
	}
	localToken := envValue(env, managedProxyTokenEnv)
	resp, err := httpPost(session.proxy.BaseURL()+"/responses", localToken)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	_ = resp.Body.Close()
	if upstreamAuth != "Bearer real-token" {
		t.Fatalf("upstream auth = %q", upstreamAuth)
	}
	report := session.closeAndReport()
	if report == nil || report.RequestCount != 1 || report.ResponseCount != 1 || report.ErrorCount != 0 || len(report.ReportJSON) == 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRegistryManagedProxyInjectsOnlyLocalEnv(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")
	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "env | sort"},
		CaptureOutput: true,
		ManagedProxy: &ManagedProxySpec{
			Provider:            "openai",
			UpstreamBaseURL:     upstream.URL,
			UpstreamBearerToken: "real-token",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	status, ok, err := registry.Wait(context.Background(), status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status=%#v ok=%v err=%v", status, ok, err)
	}
	if !strings.Contains(status.Stdout, managedProxyBaseURLEnv+"=http://127.0.0.1:") ||
		!strings.Contains(status.Stdout, managedProxyTokenEnv+"=") ||
		!strings.Contains(status.Stdout, "NO_PROXY=") ||
		strings.Contains(status.Stdout, "real-token") {
		t.Fatalf("stdout = %s", status.Stdout)
	}
	if status.ManagedProxyReport == nil {
		t.Fatalf("managed proxy report missing: %#v", status)
	}
}

func httpPost(url string, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"model":"test"}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp, nil
}

func TestRegistryStartWaitCapturesOutput(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "printf '%s' \"$AXERN_PROC\""},
		Env:           []string{"AXERN_PROC=ok"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if status.ID == "" || status.State != ProcessStateRunning {
		t.Fatalf("status = %#v", status)
	}
	status, ok, err := registry.Wait(context.Background(), status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 || strings.TrimSpace(status.Stdout) != "ok" {
		t.Fatalf("wait status = %#v", status)
	}
}

func TestRegistryCaptureOutputSeparatesStdoutAndStderr(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "printf stdout; printf stderr >&2"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	status, ok, err := registry.Wait(context.Background(), status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.Stdout != "stdout" || status.Stderr != "stderr" {
		t.Fatalf("stdout = %q, stderr = %q", status.Stdout, status.Stderr)
	}
}

func TestRegistryStartUsesBaseEnvironment(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, []string{"PATH=/usr/bin:/bin", "AXERN_BASE_ENV=base"}, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "printf '%s:%s' \"$AXERN_BASE_ENV\" \"$AXERN_OVERRIDE\""},
		Env:           []string{"AXERN_OVERRIDE=request"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	status, ok, err := registry.Wait(context.Background(), status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if got, want := strings.TrimSpace(status.Stdout), "base:request"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRegistryStartUsesBaseCwd(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	baseCwd := t.TempDir()
	resolvedBaseCwd, err := filepath.EvalSymlinks(baseCwd)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	registry := NewRegistry(waiter, nil, baseCwd)

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/pwd"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	status, ok, err := registry.Wait(context.Background(), status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if got := strings.TrimSpace(status.Stdout); got != resolvedBaseCwd {
		t.Fatalf("stdout = %q, want %q", got, resolvedBaseCwd)
	}
}

func TestProcessCwdUsesUserHomeForRootBaseCwd(t *testing.T) {
	user := processUser{home: "/home/axern"}
	if got := processCwd("", "/", user, true); got != "/home/axern" {
		t.Fatalf("cwd = %q, want user home", got)
	}
	if got := processCwd("", "/workspace", user, true); got != "/workspace" {
		t.Fatalf("cwd = %q, want image workdir", got)
	}
	if got := processCwd("/tmp/task", "/", user, true); got != "/tmp/task" {
		t.Fatalf("cwd = %q, want explicit request cwd", got)
	}
}

func TestResolveProcessUserByCurrentUID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process user resolution is platform-specific")
	}
	resolved, ok, err := resolveProcessUser(strconv.Itoa(os.Getuid()))
	if err != nil {
		t.Fatalf("resolveProcessUser() error = %v", err)
	}
	if !ok || resolved.uid != uint32(os.Getuid()) {
		t.Fatalf("resolved = %#v, ok = %v", resolved, ok)
	}
	if resolved.name != "" && len(resolved.env()) == 0 {
		t.Fatalf("resolved user env is empty for named user: %#v", resolved)
	}
}

func TestResolveNumericProcessUserDefaults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process user resolution is platform-specific")
	}
	resolved, ok, err := resolveProcessUser("4242424242")
	if err != nil {
		t.Fatalf("resolveProcessUser() error = %v", err)
	}
	if !ok || resolved.uid != 4242424242 || resolved.name != "4242424242" || resolved.defaultCwd() != "/" {
		t.Fatalf("resolved = %#v, ok = %v", resolved, ok)
	}
	if got := resolved.env(); !reflect.DeepEqual(got, []string{"HOME=/", "USER=4242424242", "LOGNAME=4242424242"}) {
		t.Fatalf("env = %#v", got)
	}
}

func TestParseProcessUsersAndGroups(t *testing.T) {
	passwd := parsePasswd([]byte("root:x:0:0:root:/root:/bin/sh\naxern:x:1000:1001::/home/axern:/bin/bash\n"))
	entry, ok := findPasswdByName(passwd, "axern")
	if !ok {
		t.Fatalf("expected axern passwd entry")
	}
	if entry.uid != 1000 || entry.gid != 1001 || entry.home != "/home/axern" || entry.shell != "/bin/bash" {
		t.Fatalf("entry = %#v", entry)
	}
	groups := parseGroups([]byte("users:x:100:axern,other\nwheel:x:10:root\n"))
	if got := supplementaryGroups("axern", groups); !reflect.DeepEqual(got, []uint32{100}) {
		t.Fatalf("supplementary groups = %#v, want [100]", got)
	}
}

func TestRegistryListProcesses(t *testing.T) {
	registry := NewRegistry(nil, nil, "")
	registry.procs["proc-10"] = &managedProcess{status: Status{ID: "proc-10", State: ProcessStateRunning, PID: 10}}
	registry.procs["proc-2"] = &managedProcess{status: Status{ID: "proc-2", State: ProcessStateExited, PID: 2}}
	registry.procs["custom"] = &managedProcess{status: Status{ID: "custom", State: ProcessStateFailed}}

	list := registry.List()
	if len(list.Processes) != 3 {
		t.Fatalf("process count = %d, want 3", len(list.Processes))
	}
	got := []string{list.Processes[0].ID, list.Processes[1].ID, list.Processes[2].ID}
	want := []string{"custom", "proc-2", "proc-10"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("process order = %#v, want %#v", got, want)
		}
	}
}

func TestRegistryStreamsOutputAndWritesStdin(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:         []string{"/bin/sh", "-c", "cat; printf ':err' >&2"},
		OpenStdin:    true,
		StreamOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, ok, err := registry.SubscribeOutput(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("SubscribeOutput() ok = %v, err = %v", ok, err)
	}
	if _, ok, err := registry.WriteStdin(status.ID, []byte("ok")); err != nil || !ok {
		t.Fatalf("WriteStdin() ok = %v, err = %v", ok, err)
	}
	if _, ok, err := registry.CloseStdin(status.ID); err != nil || !ok {
		t.Fatalf("CloseStdin() ok = %v, err = %v", ok, err)
	}
	status, ok, err = registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	var stdout, stderr strings.Builder
	for event := range events {
		stdout.Write(event.Stdout)
		stderr.Write(event.Stderr)
	}
	if stdout.String() != "ok" || stderr.String() != ":err" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRegistryCloseStdinIsIdempotentAndWriteFailsAfterClose(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:      []string{"/bin/sh", "-c", "cat >/dev/null"},
		OpenStdin: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, ok, err := registry.CloseStdin(status.ID); err != nil || !ok {
		t.Fatalf("CloseStdin() ok=%v err=%v", ok, err)
	}
	if _, ok, err := registry.CloseStdin(status.ID); err != nil || !ok {
		t.Fatalf("second CloseStdin() ok=%v err=%v", ok, err)
	}
	if _, ok, err := registry.WriteStdin(status.ID, []byte("late")); err == nil || !ok {
		t.Fatalf("WriteStdin() after close ok=%v err=%v, want error", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status=%#v ok=%v err=%v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestRegistryStreamsCompletedOutputToLateSubscriber(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:         []string{"/bin/sh", "-c", "printf late-out; printf late-err >&2"},
		StreamOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	events, ok, err := registry.SubscribeOutput(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("SubscribeOutput() ok = %v, err = %v", ok, err)
	}
	var stdout, stderr strings.Builder
	for event := range events {
		stdout.Write(event.Stdout)
		stderr.Write(event.Stderr)
	}
	if stdout.String() != "late-out" || stderr.String() != "late-err" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRegistryStreamCancelDoesNotAffectProcessWait(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:         []string{"/bin/sh", "-c", "printf before; sleep 0.1; printf after"},
		StreamOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	streamCtx, cancelStream := context.WithCancel(context.Background())
	events, ok, err := registry.SubscribeOutput(streamCtx, status.ID)
	if err != nil || !ok {
		t.Fatalf("SubscribeOutput() ok = %v, err = %v", ok, err)
	}
	cancelStream()
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDrain()
	drained := false
	for !drained {
		select {
		case _, ok := <-events:
			if !ok {
				drained = true
			}
		case <-drainCtx.Done():
			t.Fatalf("stream did not close after cancel: %v", drainCtx.Err())
		}
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	waited, ok, err := registry.Wait(waitCtx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", waited, ok, err)
	}
	if waited.ExitCode == nil || *waited.ExitCode != 0 {
		t.Fatalf("waited = %#v", waited)
	}
	again, ok, err := registry.Wait(waitCtx, status.ID)
	if err != nil || !ok || again.State != ProcessStateExited {
		t.Fatalf("second Wait() status = %#v, ok = %v, err = %v", again, ok, err)
	}
	current, ok := registry.Status(status.ID)
	if !ok || current.State != ProcessStateExited || current.ExitCode == nil || *current.ExitCode != 0 {
		t.Fatalf("Status() = %#v, ok = %v", current, ok)
	}
}

func TestRegistryTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group timeout behavior is platform-specific")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "sleep 30"},
		CaptureOutput: true,
		TimeoutMs:     50,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.State != ProcessStateExited || status.ExitCode == nil || *status.ExitCode != 137 {
		t.Fatalf("status = %#v", status)
	}
	if status.Signal == "" {
		t.Fatalf("signal = %q, want timeout kill signal", status.Signal)
	}
	if !strings.Contains(status.LastError, "process timed out after") {
		t.Fatalf("last error = %q, want timeout", status.LastError)
	}
}

func TestRegistryShutdownTerminatesActiveProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group shutdown behavior is platform-specific")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "trap '' TERM; sleep 30"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := registry.Shutdown(ctx, 10*time.Millisecond); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.State != ProcessStateExited || status.ExitCode == nil {
		t.Fatalf("status = %#v, want exited process", status)
	}
	for _, item := range registry.List().Processes {
		if item.State == ProcessStateRunning || item.State == ProcessStateStarting {
			t.Fatalf("process %s still active after shutdown: %#v", item.ID, item)
		}
	}
}

func TestRegistryShutdownUsesTermBeforeKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group shutdown behavior is platform-specific")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "trap 'exit 42' TERM; while :; do :; done"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := registry.Shutdown(ctx, 2*time.Second); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode == 137 {
		t.Fatalf("status = %#v, want TERM-driven exit before forced kill", status)
	}
}

func TestRegistryShutdownHonorsCanceledContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group shutdown behavior is platform-specific")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{Args: []string{"/bin/sh", "-c", "trap '' TERM; sleep 30"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.Shutdown(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() error = %v, want context canceled", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	status, ok, err := registry.Wait(waitCtx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.State != ProcessStateExited {
		t.Fatalf("status = %#v, want exited after forced cleanup", status)
	}
}

func TestRegistryRejectsNegativeTimeout(t *testing.T) {
	registry := NewRegistry(nil, nil, "")
	if _, err := registry.Start(StartRequest{Args: []string{"/bin/true"}, TimeoutMs: -1}); err == nil {
		t.Fatal("Start() error = nil, want invalid timeout")
	}
}

func TestRegistryRejectsOverflowingTimeout(t *testing.T) {
	registry := NewRegistry(nil, nil, "")
	if _, err := registry.Start(StartRequest{Args: []string{"/bin/true"}, TimeoutMs: 1<<63 - 1}); err == nil {
		t.Fatal("Start() error = nil, want invalid timeout")
	}
}

func TestRegistryLimitsActiveProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell commands")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")
	var ids []string
	for i := 0; i < maxActiveProcesses; i++ {
		status, err := registry.Start(StartRequest{Args: []string{"/bin/sh", "-c", "sleep 30"}})
		if err != nil {
			t.Fatalf("Start(%d) error = %v", i, err)
		}
		ids = append(ids, status.ID)
	}
	if _, err := registry.Start(StartRequest{Args: []string{"/bin/sh", "-c", "sleep 30"}}); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Start() error = %v, want resource limit", err)
	}
	signal, err := ParseSignal("KILL")
	if err != nil {
		t.Fatalf("ParseSignal(KILL) error = %v", err)
	}
	for _, id := range ids {
		if _, _, err := registry.Signal(id, signal); err != nil {
			t.Fatalf("Signal(%s) error = %v", id, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range ids {
		if _, ok, err := registry.Wait(ctx, id); err != nil || !ok {
			t.Fatalf("Wait(%s) ok=%v err=%v", id, ok, err)
		}
	}
}

func TestRegistryTerminalCapturesOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("terminal process behavior is covered by Linux e2e")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh"},
		Stdin:         "printf 'pty-ok'; exit\r",
		CaptureOutput: true,
		Terminal:      true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 || !strings.Contains(status.Stdout, "pty-ok") {
		t.Fatalf("terminal status = %#v", status)
	}
}

func TestRegistryTerminalOutputReturnsToColumnStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("terminal process behavior is covered by Linux e2e")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-c", "printf 'alpha\\nbeta\\n'"},
		CaptureOutput: true,
		Terminal:      true,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("terminal status = %#v", status)
	}
	if !strings.Contains(status.Stdout, "alpha\r\nbeta\r\n") {
		t.Fatalf("terminal stdout = %q, want CRLF line discipline", status.Stdout)
	}
}

func TestRegistryTerminalStartsWithInitialSize(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("terminal process behavior is covered by Linux e2e")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh", "-lc", "stty size"},
		CaptureOutput: true,
		Terminal:      true,
		InitialCols:   101,
		InitialRows:   33,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 || !strings.Contains(status.Stdout, "33 101") {
		t.Fatalf("terminal status = %#v", status)
	}
}

func TestRegistryTerminalResizeUpdatesRunningTTY(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("terminal process behavior is covered by Linux e2e")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{
		Args:          []string{"/bin/sh"},
		CaptureOutput: true,
		OpenStdin:     true,
		Terminal:      true,
		InitialCols:   80,
		InitialRows:   24,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, ok, err := registry.Resize(status.ID, 111, 37); err != nil || !ok {
		t.Fatalf("Resize() ok=%v err=%v", ok, err)
	}
	if _, ok, err := registry.WriteStdin(status.ID, []byte("stty size; exit\r")); err != nil || !ok {
		t.Fatalf("WriteStdin() ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 || !strings.Contains(status.Stdout, "37 111") {
		t.Fatalf("terminal status = %#v", status)
	}
	if _, _, err := registry.Resize(status.ID, 120, 40); err == nil {
		t.Fatal("Resize() after exit error = nil")
	}
}

func TestTerminalSizeRejectsInvalidDimensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		cols uint32
		rows uint32
	}{
		{name: "missing cols", cols: 0, rows: 24},
		{name: "missing rows", cols: 80, rows: 0},
		{name: "cols overflow", cols: maxTerminalDimension + 1, rows: 24},
		{name: "rows overflow", cols: 80, rows: maxTerminalDimension + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := terminalSize(tc.cols, tc.rows); err == nil {
				t.Fatal("terminalSize() error = nil")
			}
		})
	}
}

func TestManagedProcessWriteStdinHandlesShortWrites(t *testing.T) {
	writer := &shortWriteCloser{max: 2}
	managed := &managedProcess{stdin: writer}
	if err := managed.writeStdin([]byte("abcdef")); err != nil {
		t.Fatalf("writeStdin() error = %v", err)
	}
	if got := writer.data.String(); got != "abcdef" {
		t.Fatalf("stdin = %q, want abcdef", got)
	}
}

func TestManagedProcessCloseStdinLeavesTerminalOpen(t *testing.T) {
	writer := &shortWriteCloser{max: 8}
	managed := &managedProcess{
		stdin:    writer,
		terminal: true,
	}
	if err := managed.closeStdin(); err != nil {
		t.Fatalf("closeStdin() error = %v", err)
	}
	if writer.closed {
		t.Fatal("terminal stdin was closed")
	}
	if managed.stdin == nil {
		t.Fatal("terminal stdin was cleared")
	}
}

func TestRegistryStartFailure(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{Args: []string{"/tmp/axern-missing-process"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if status.State != ProcessStateFailed || status.ExitCode == nil || *status.ExitCode != proc.RuntimeStartExitCode {
		t.Fatalf("status = %#v", status)
	}
}

func TestRegistryRetainsBoundedCompletedProcesses(t *testing.T) {
	registry := NewRegistry(nil, nil, "")
	for i := 0; i < maxRetainedProcesses+3; i++ {
		id := "proc-" + strconv.Itoa(i)
		registry.procs[id] = &managedProcess{status: Status{ID: id, State: ProcessStateExited}}
		registry.recordDone(id)
	}
	if len(registry.procs) != maxRetainedProcesses {
		t.Fatalf("retained processes = %d, want %d", len(registry.procs), maxRetainedProcesses)
	}
	if _, ok := registry.procs["proc-0"]; ok {
		t.Fatal("oldest completed process was not evicted")
	}
	if _, ok := registry.procs["proc-257"]; !ok {
		t.Fatal("newest completed process was evicted")
	}
}

func TestLimitedBufferCapsCapturedOutput(t *testing.T) {
	buffer := newLimitedBuffer(4)
	n, err := buffer.Write([]byte("abcdef"))
	if err != nil || n != 6 {
		t.Fatalf("Write() n = %d, err = %v", n, err)
	}
	if got := buffer.String(); got != "abcd" {
		t.Fatalf("buffer = %q, want abcd", got)
	}
	if !buffer.Truncated() {
		t.Fatal("buffer was not marked truncated")
	}
}

func TestRegistrySignal(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process group signal behavior is covered by Linux e2e")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "")

	status, err := registry.Start(StartRequest{Args: []string{"/bin/sh", "-c", "while true; do sleep 1; done"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, ok, err := registry.Signal(status.ID, mustSignal(t, "TERM")); err != nil || !ok {
		t.Fatalf("Signal() ok = %v, err = %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, ok, err := registry.Wait(ctx, status.ID)
	if err != nil || !ok {
		t.Fatalf("Wait() status = %#v, ok = %v, err = %v", status, ok, err)
	}
	if status.ExitCode == nil || *status.ExitCode != 143 {
		t.Fatalf("status = %#v, want exit 143", status)
	}
}

func mustSignal(t *testing.T, value string) os.Signal {
	t.Helper()
	sig, err := ParseSignal(value)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

type shortWriteCloser struct {
	max    int
	data   strings.Builder
	closed bool
}

func (w *shortWriteCloser) Write(data []byte) (int, error) {
	if len(data) > w.max {
		data = data[:w.max]
	}
	return w.data.WriteString(string(data))
}

func (w *shortWriteCloser) Close() error {
	w.closed = true
	return nil
}
