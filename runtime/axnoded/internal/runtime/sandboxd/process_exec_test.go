package sandboxd

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestSandboxdExecContainer(t *testing.T) {
	client := &fakeSandboxdProcessClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait: ProcessStatus{
			ID:              "proc-1",
			State:           "exited",
			ExitCode:        intPtr(7),
			Stdout:          "out",
			Stderr:          "err",
			StdoutTruncated: true,
		},
	}
	restore := replaceSandboxdProcessClient(t, client)
	defer restore()
	containerRoot := t.TempDir()
	containerID := "exec-test"

	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"/bin/sh", "-c", "echo ok"},
		Envs: []*apipb.KeyValue{
			{Key: "B", Value: "2"},
			{Key: "A", Value: "1"},
		},
		Cwd: "/tmp",
	}, processTestOptions(containerID), containerRoot)
	if err != nil {
		t.Fatalf("ExecContainer() error = %v", err)
	}
	if response.ExitCode != 7 || string(response.Stdout) != "out" || string(response.Stderr) != "err" || !response.StdoutTruncated {
		t.Fatalf("response = %#v", response)
	}
	wantSocket := runtimeoci.SandboxdBundleSocketPath(filepath.Join(containerRoot, containerID))
	if client.socketPath != wantSocket {
		t.Fatalf("socketPath = %q, want %q", client.socketPath, wantSocket)
	}
	if got, want := client.startRequest.Env, []string{"A=1", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
	if client.startRequest.Cwd != "/tmp" || !client.startRequest.CaptureOutput {
		t.Fatalf("start request = %#v", client.startRequest)
	}
}

func TestSandboxdExecContainerRequiresContainerID(t *testing.T) {
	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
	}, contract.HandlerOptions{}, t.TempDir())
	if response != nil || !errors.Is(err, errord.ErrInvalidArgument) {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
}

func TestSandboxdExecContainerRequiresProcessCapability(t *testing.T) {
	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
	}, contract.HandlerOptions{
		ContainerID: "exec-test",
		ContainerLabels: map[string]string{
			LabelReady:        "true",
			LabelSocket:       "/tmp/sandboxd.sock",
			LabelCapabilities: "file",
		},
	}, t.TempDir())
	if response != nil || !errors.Is(err, errord.ErrFailedPrecondition) {
		t.Fatalf("response = %#v, err = %v, want failed precondition", response, err)
	}
}

func TestSandboxdExecContainerForwardsUser(t *testing.T) {
	client := &fakeSandboxdProcessClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait:  ProcessStatus{ID: "proc-1", State: "exited", ExitCode: intPtr(0)},
	}
	restore := replaceSandboxdProcessClient(t, client)
	defer restore()

	_, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
		User:    "1000",
	}, processTestOptions("exec-test"), t.TempDir())
	if err != nil {
		t.Fatalf("ExecContainer() error = %v", err)
	}
	if client.startRequest.User != "1000" {
		t.Fatalf("user = %q, want 1000", client.startRequest.User)
	}
}

func TestSandboxdExecContainerForwardsManagedProxy(t *testing.T) {
	client := &fakeSandboxdProcessClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait: ProcessStatus{
			ID:       "proc-1",
			State:    "exited",
			ExitCode: intPtr(0),
			ManagedProxyReport: &ManagedProxyReport{
				Provider:      "openai",
				RequestCount:  1,
				ResponseCount: 1,
				ReportJSON:    []byte(`{"provider":"openai"}`),
			},
		},
	}
	restore := replaceSandboxdProcessClient(t, client)
	defer restore()

	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
		ManagedProxy: &apipb.ManagedProxySpec{
			Provider:            "openai",
			UpstreamBaseUrl:     "https://api.example.test/v1",
			UpstreamBearerToken: "secret-token",
		},
	}, processTestOptionsWithCapabilities("exec-managed-proxy-test", "process,pty,managed_proxy"), t.TempDir())
	if err != nil {
		t.Fatalf("ExecContainer() error = %v", err)
	}
	if client.startRequest.ManagedProxy == nil ||
		client.startRequest.ManagedProxy.Provider != "openai" ||
		client.startRequest.ManagedProxy.UpstreamBaseURL != "https://api.example.test/v1" ||
		client.startRequest.ManagedProxy.UpstreamBearerToken != "secret-token" {
		t.Fatalf("managed proxy start request = %#v", client.startRequest.ManagedProxy)
	}
	if response.GetManagedProxyReport().GetProvider() != "openai" ||
		response.GetManagedProxyReport().GetRequestCount() != 1 ||
		response.GetManagedProxyReport().GetResponseCount() != 1 ||
		string(response.GetManagedProxyReport().GetReportJson()) != `{"provider":"openai"}` {
		t.Fatalf("managed proxy response = %#v", response.GetManagedProxyReport())
	}
}

func TestSandboxdExecContainerRequiresManagedProxyCapability(t *testing.T) {
	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
		ManagedProxy: &apipb.ManagedProxySpec{
			Provider:        "openai",
			UpstreamBaseUrl: "https://api.example.test/v1",
		},
	}, processTestOptions("exec-managed-proxy-missing-capability"), t.TempDir())
	if response != nil || !errors.Is(err, errord.ErrFailedPrecondition) {
		t.Fatalf("response = %#v, err = %v, want failed precondition", response, err)
	}
}

func TestSandboxdExecContainerStartsTerminal(t *testing.T) {
	client := &fakeSandboxdProcessClient{
		start: ProcessStatus{ID: "proc-1", State: "running"},
		wait:  ProcessStatus{ID: "proc-1", State: "exited", ExitCode: intPtr(0), Stdout: "tty"},
	}
	restore := replaceSandboxdProcessClient(t, client)
	defer restore()

	response, err := ExecContainer(context.Background(), &apipb.ExecContainerRequest{
		Command: []string{"true"},
		Tty:     true,
	}, processTestOptions("exec-tty-test"), t.TempDir())
	if err != nil {
		t.Fatalf("ExecContainer() error = %v", err)
	}
	if response.GetExitCode() != 0 || string(response.GetStdout()) != "tty" {
		t.Fatalf("response = %#v", response)
	}
	if !client.startRequest.Terminal || !client.startRequest.CaptureOutput {
		t.Fatalf("start request = %#v", client.startRequest)
	}
}

func intPtr(value int) *int {
	return &value
}

func processTestOptions(containerID string) contract.HandlerOptions {
	return processTestOptionsWithCapabilities(containerID, "process,pty")
}

func processTestOptionsWithCapabilities(containerID string, capabilities string) contract.HandlerOptions {
	return contract.HandlerOptions{
		ContainerID: containerID,
		ContainerLabels: map[string]string{
			LabelReady:        "true",
			LabelSocket:       "/tmp/sandboxd.sock",
			LabelCapabilities: capabilities,
		},
	}
}

type fakeSandboxdProcessClient struct {
	start ProcessStatus
	wait  ProcessStatus

	started      bool
	socketPath   string
	startRequest ProcessStartRequest
}

func (f *fakeSandboxdProcessClient) StartProcess(_ context.Context, request ProcessStartRequest) (ProcessStatus, error) {
	f.started = true
	f.startRequest = request
	return f.start, nil
}

func (f *fakeSandboxdProcessClient) WaitProcess(context.Context, string) (ProcessStatus, error) {
	return f.wait, nil
}

func replaceSandboxdProcessClient(t *testing.T, client *fakeSandboxdProcessClient) func() {
	t.Helper()
	previous := newProcessClient
	currentFake := client
	newProcessClient = func(socketPath string) processClient {
		currentFake.socketPath = socketPath
		return currentFake
	}
	return func() {
		newProcessClient = previous
	}
}
