package sandbox

import (
	"testing"

	nodeoperatorv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/operator/v1"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

type fakeKillRPCClient struct {
	lastAllocationID string
	lastReason       string
	killErr          error
}

func (f *fakeKillRPCClient) ForceTerminateAllocation(allocationID, reason string) (*nodeoperatorv1.ForceTerminateAllocationResponse, error) {
	f.lastAllocationID = allocationID
	f.lastReason = reason
	return &nodeoperatorv1.ForceTerminateAllocationResponse{}, f.killErr
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

func TestForceTerminateRequiresReason(t *testing.T) {
	fakeClient := &fakeKillRPCClient{}
	oldFactory := newKillRPCClient
	newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) { return fakeClient, nil }
	defer func() { newKillRPCClient = oldFactory }()

	err := newKillTestApp().Run([]string{"axctl", "force-terminate", "axctl-test"})

	assert.EqualError(t, err, "--reason is required")
	assert.Empty(t, fakeClient.lastAllocationID)
}

func TestForceTerminatePassesAuditedReason(t *testing.T) {
	fakeClient := &fakeKillRPCClient{}
	oldFactory := newKillRPCClient
	newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) { return fakeClient, nil }
	defer func() { newKillRPCClient = oldFactory }()

	err := newKillTestApp().Run([]string{"axctl", "force-terminate", "--reason", "incident-42", "axctl-test"})

	assert.NoError(t, err)
	assert.Equal(t, "axctl-test", fakeClient.lastAllocationID)
	assert.Equal(t, "incident-42", fakeClient.lastReason)
}
