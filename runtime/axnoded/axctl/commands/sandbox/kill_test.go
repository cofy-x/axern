package sandbox

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

type fakeKillRPCClient struct {
	lastSandboxID string
	lastSignal    string
	killErr       error
}

func (f *fakeKillRPCClient) KillSandbox(sandboxID, signal string) (*nodeoperatorv1.KillSandboxResponse, error) {
	f.lastSandboxID = sandboxID
	f.lastSignal = signal
	return &nodeoperatorv1.KillSandboxResponse{}, f.killErr
}

func (f *fakeKillRPCClient) Close() error { return nil }

func newKillTestApp() *cli.App {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "address", Value: config.DefaultSocketAddress},
		cli.DurationFlag{Name: "timeout", Value: config.DefaultTimeout},
	}
	app.Commands = []cli.Command{KillCmd}
	return app
}

func TestKillCommandUsesDefaultSignal(t *testing.T) {
	fakeClient := &fakeKillRPCClient{}
	oldFactory := newKillRPCClient
	newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) { return fakeClient, nil }
	defer func() { newKillRPCClient = oldFactory }()

	err := newKillTestApp().Run([]string{"axctl", "kill", "axctl-test"})

	assert.NoError(t, err)
	assert.Equal(t, "axctl-test", fakeClient.lastSandboxID)
	assert.Equal(t, "TERM", fakeClient.lastSignal)
}

func TestKillCommandPassesExplicitSignal(t *testing.T) {
	fakeClient := &fakeKillRPCClient{}
	oldFactory := newKillRPCClient
	newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) { return fakeClient, nil }
	defer func() { newKillRPCClient = oldFactory }()

	err := newKillTestApp().Run([]string{"axctl", "kill", "--signal", "SIGKILL", "axctl-test"})

	assert.NoError(t, err)
	assert.Equal(t, "axctl-test", fakeClient.lastSandboxID)
	assert.Equal(t, "SIGKILL", fakeClient.lastSignal)
}
