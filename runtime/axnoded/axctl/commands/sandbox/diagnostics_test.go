package sandbox

import (
	"bytes"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeDiagnosticsRPCClient struct {
	lastSandboxID string
	lastFull      bool
	resp          *nodeoperatorv1.GetSandboxDiagnosticsResponse
	err           error
}

func (f *fakeDiagnosticsRPCClient) GetSandboxDiagnostics(sandboxID string, full bool) (*nodeoperatorv1.GetSandboxDiagnosticsResponse, error) {
	f.lastSandboxID = sandboxID
	f.lastFull = full
	if f.resp != nil || f.err != nil {
		return f.resp, f.err
	}
	return &nodeoperatorv1.GetSandboxDiagnosticsResponse{SandboxID: sandboxID, Ready: true}, nil
}

func (f *fakeDiagnosticsRPCClient) Close() error { return nil }

func newDiagnosticsTestApp() *cli.App {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "address", Value: config.DefaultSocketAddress},
		cli.DurationFlag{Name: "timeout", Value: config.DefaultTimeout},
	}
	app.Commands = []cli.Command{DiagnosticsCmd}
	return app
}

func TestDiagnosticsCommandRequestsFullForJSON(t *testing.T) {
	fakeClient := &fakeDiagnosticsRPCClient{resp: &nodeoperatorv1.GetSandboxDiagnosticsResponse{RawJson: `{"ready":true}`}}
	oldFactory := newDiagnosticsRPCClient
	newDiagnosticsRPCClient = func(ctx *cli.Context) (diagnosticsRPCClient, error) { return fakeClient, nil }
	defer func() { newDiagnosticsRPCClient = oldFactory }()

	err := newDiagnosticsTestApp().Run([]string{"axctl", "diagnostics", "--json", "axctl-test"})

	assert.NoError(t, err)
	assert.Equal(t, "axctl-test", fakeClient.lastSandboxID)
	assert.True(t, fakeClient.lastFull)
}

func TestRenderSandboxDiagnostics(t *testing.T) {
	var out bytes.Buffer
	renderSandboxDiagnostics(&out, &nodeoperatorv1.GetSandboxDiagnosticsResponse{
		SandboxID:     "demo",
		Ready:         true,
		Detail:        "full",
		GeneratedAt:   timestamppb.New(time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)),
		DaemonPid:     42,
		UptimeSeconds: 3.5,
		SocketPath:    "/tmp/sandboxd.sock",
		UserState:     "running",
		Capabilities:  []string{"health", "process"},
		ProviderSummary: &nodeoperatorv1.SandboxdProviderSummary{
			Total:     1,
			Available: 1,
		},
		ProcessSummary: &nodeoperatorv1.SandboxdProcessSummary{
			Total:   2,
			Running: 1,
			Exited:  1,
		},
		Providers: []*nodeoperatorv1.SandboxdProvider{
			{
				Name:         "process",
				State:        "available",
				Available:    true,
				Capabilities: []string{"process"},
				Dependencies: []*nodeoperatorv1.SandboxdProviderDependency{{Name: "procfs", Available: true}},
			},
		},
	})

	got := out.String()
	for _, want := range []string{
		"Sandbox: demo",
		"Ready: true",
		"Generated At: 2026-05-27T10:00:00Z",
		"Capabilities: health,process",
		"Providers: 1 total, 1 available, 0 degraded, 0 unavailable",
		"Processes: 2 total, 0 starting, 1 running, 1 exited, 0 failed",
		"dependency procfs: available=true",
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("renderSandboxDiagnostics() missing %q:\n%s", want, got)
		}
	}
}
