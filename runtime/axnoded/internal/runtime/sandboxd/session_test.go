package sandboxd

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestOpenExecSession(t *testing.T) {
	client := &fakeSessionClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait:  ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
		stream: []ProcessStreamEvent{
			{Stdout: []byte("out")},
			{Stderr: []byte("err")},
		},
	}
	restore := replaceSessionClient(t, client)
	defer restore()
	containerRoot := t.TempDir()
	containerID := "session-test"

	session, err := OpenExecSession(context.Background(), &apipb.ExecSessionOpen{
		Command: []string{"/bin/sh", "-c", "cat"},
		Envs: []*apipb.KeyValue{
			{Key: "B", Value: "2"},
			{Key: "A", Value: "1"},
		},
		Cwd:  "/tmp",
		User: "axern",
	}, processTestOptions(containerID), containerRoot)
	if err != nil {
		t.Fatalf("OpenExecSession() error = %v", err)
	}
	if err := session.Write([]byte("payload")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := session.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin() error = %v", err)
	}
	if err := session.Signal("TERM"); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}

	var chunks []contract.Chunk
	for {
		chunk, err := session.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv() error = %v", err)
		}
		chunks = append(chunks, chunk)
	}
	exit, err := session.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if exit.Status != 0 {
		t.Fatalf("exit = %#v", exit)
	}
	wantSocket := runtimeoci.SandboxdBundleSocketPath(filepath.Join(containerRoot, containerID))
	if client.socketPath != wantSocket {
		t.Fatalf("socketPath = %q, want %q", client.socketPath, wantSocket)
	}
	if got, want := client.startRequest.Env, []string{"A=1", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
	if !client.startRequest.OpenStdin || !client.startRequest.StreamOutput || client.startRequest.Cwd != "/tmp" || client.startRequest.User != "axern" {
		t.Fatalf("start request = %#v", client.startRequest)
	}
	if string(client.stdin) != "payload" || !client.stdinClosed || client.signal != "TERM" {
		t.Fatalf("client stdin = %q, closed = %v, signal = %q", string(client.stdin), client.stdinClosed, client.signal)
	}
	if len(chunks) != 2 || string(chunks[0].Stdout) != "out" || string(chunks[1].Stderr) != "err" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestOpenExecSessionStartsTerminal(t *testing.T) {
	client := &fakeSessionClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait:  ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
	}
	restore := replaceSessionClient(t, client)
	defer restore()

	session, err := OpenExecSession(context.Background(), &apipb.ExecSessionOpen{
		Command: []string{"true"},
		Tty:     true,
		InitialSize: &apipb.TerminalResize{
			Cols: 101,
			Rows: 33,
		},
	}, processTestOptions("session-tty-test"), t.TempDir())
	if err != nil {
		t.Fatalf("OpenExecSession() error = %v", err)
	}
	defer session.Close()
	if !client.startRequest.Terminal || !client.startRequest.OpenStdin || !client.startRequest.StreamOutput {
		t.Fatalf("start request = %#v", client.startRequest)
	}
	if client.startRequest.InitialCols != 101 || client.startRequest.InitialRows != 33 {
		t.Fatalf("initial size = %dx%d, want 101x33", client.startRequest.InitialCols, client.startRequest.InitialRows)
	}
}

func TestSessionWaitReturnsStreamError(t *testing.T) {
	streamErr := errors.New("stream failed")
	session := NewSession(context.Background(), &fakeSessionClient{
		wait:      ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
		streamErr: streamErr,
	}, "proc-1")
	_, err := session.Wait()
	if err == nil || !strings.Contains(err.Error(), streamErr.Error()) {
		t.Fatalf("Wait() error = %v, want stream error", err)
	}
}

func TestSessionWaitDrainsStreamBeforeExit(t *testing.T) {
	client := &fakeSessionClient{
		wait:        ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
		waitDelay:   0,
		streamDelay: 50 * time.Millisecond,
		stream: []ProcessStreamEvent{
			{Stdout: []byte("fast-out")},
		},
	}
	session := NewSession(context.Background(), client, "proc-1")

	exit, err := session.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if exit.Status != 0 {
		t.Fatalf("exit = %#v", exit)
	}
	chunk, err := session.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if string(chunk.Stdout) != "fast-out" {
		t.Fatalf("stdout = %q, want fast-out", string(chunk.Stdout))
	}
	if _, err := session.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("Recv() error = %v, want EOF", err)
	}
}

func TestSessionCloseSignalsWithCanceledParentContext(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	client := &fakeSessionClient{
		wait: ProcessStatus{ID: "proc-1", State: "running"},
	}
	session := NewSession(parentCtx, client, "proc-1")
	cancel()
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !client.stdinClosed || client.signal != "TERM" {
		t.Fatalf("stdinClosed = %v, signal = %q", client.stdinClosed, client.signal)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	client := &fakeSessionClient{
		wait: ProcessStatus{ID: "proc-1", State: "running"},
	}
	session := NewSession(context.Background(), client, "proc-1")
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if client.closeStdinCalls != 1 || client.signalCalls != 1 {
		t.Fatalf("closeStdinCalls = %d, signalCalls = %d, want 1 each", client.closeStdinCalls, client.signalCalls)
	}
}

func TestSessionCloseEscalatesAfterGracePeriod(t *testing.T) {
	previous := sessionShutdownTimeout
	sessionShutdownTimeout = 10 * time.Millisecond
	t.Cleanup(func() { sessionShutdownTimeout = previous })

	client := &fakeSessionClient{
		wait:      ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(137)},
		waitDelay: 200 * time.Millisecond,
	}
	session := NewSession(context.Background(), client, "proc-1")
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := strings.Join(client.signals, ","), "TERM,KILL"; got != want {
		t.Fatalf("signals = %q, want %q", got, want)
	}
}

func TestSessionCloseDoesNotKillAfterGracefulExit(t *testing.T) {
	previous := sessionShutdownTimeout
	sessionShutdownTimeout = time.Second
	t.Cleanup(func() { sessionShutdownTimeout = previous })

	client := &fakeSessionClient{
		wait: ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
	}
	session := NewSession(context.Background(), client, "proc-1")
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := strings.Join(client.signals, ","), "TERM"; got != want {
		t.Fatalf("signals = %q, want %q", got, want)
	}
	exit, err := session.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if exit.Status != 0 {
		t.Fatalf("exit = %#v", exit)
	}
}

func TestSessionResizeForwardsTerminalSize(t *testing.T) {
	client := &fakeSessionClient{
		wait: ProcessStatus{ID: "proc-1", State: "exited", ExitCode: sessionIntPtr(0)},
	}
	session := NewSession(context.Background(), client, "proc-1")
	defer session.Close()

	if err := session.Resize(101, 33); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if client.resizeCols != 101 || client.resizeRows != 33 {
		t.Fatalf("resize = %dx%d, want 101x33", client.resizeCols, client.resizeRows)
	}
}

func sessionIntPtr(value int) *int {
	return &value
}

type fakeSessionClient struct {
	start       ProcessStatus
	wait        ProcessStatus
	stream      []ProcessStreamEvent
	streamErr   error
	waitDelay   time.Duration
	streamDelay time.Duration

	socketPath      string
	startRequest    ProcessStartRequest
	stdin           []byte
	stdinClosed     bool
	closeStdinCalls int
	resizeCols      uint32
	resizeRows      uint32
	signal          string
	signals         []string
	signalCalls     int
}

func (f *fakeSessionClient) StartProcess(_ context.Context, request ProcessStartRequest) (ProcessStatus, error) {
	f.startRequest = request
	return f.start, nil
}

func (f *fakeSessionClient) WriteProcessStdin(_ context.Context, _ string, data []byte) (ProcessStatus, error) {
	f.stdin = append(f.stdin, data...)
	return ProcessStatus{ID: "proc-1", State: "running"}, nil
}

func (f *fakeSessionClient) CloseProcessStdin(context.Context, string) (ProcessStatus, error) {
	f.closeStdinCalls++
	f.stdinClosed = true
	return ProcessStatus{ID: "proc-1", State: "running"}, nil
}

func (f *fakeSessionClient) ResizeProcess(_ context.Context, _ string, cols uint32, rows uint32) (ProcessStatus, error) {
	f.resizeCols = cols
	f.resizeRows = rows
	return ProcessStatus{ID: "proc-1", State: "running"}, nil
}

func (f *fakeSessionClient) SignalProcess(_ context.Context, _ string, signal string) (ProcessStatus, error) {
	f.signalCalls++
	f.signal = signal
	f.signals = append(f.signals, signal)
	return ProcessStatus{ID: "proc-1", State: "running"}, nil
}

func (f *fakeSessionClient) StreamProcess(ctx context.Context, _ string, emit func(ProcessStreamEvent) error) error {
	if f.streamErr != nil {
		return f.streamErr
	}
	for _, event := range f.stream {
		if f.streamDelay > 0 {
			timer := time.NewTimer(f.streamDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := emit(event); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSessionClient) WaitProcess(context.Context, string) (ProcessStatus, error) {
	if f.waitDelay > 0 {
		time.Sleep(f.waitDelay)
	}
	return f.wait, nil
}

func replaceSessionClient(t *testing.T, client *fakeSessionClient) func() {
	t.Helper()
	previous := NewSessionClient
	currentFake := client
	NewSessionClient = func(socketPath string) SessionClient {
		currentFake.socketPath = socketPath
		return currentFake
	}
	return func() {
		NewSessionClient = previous
	}
}
