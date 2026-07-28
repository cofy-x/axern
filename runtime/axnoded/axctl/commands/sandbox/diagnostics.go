package sandbox

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
)

type diagnosticsRPCClient interface {
	GetSandboxDiagnostics(sandboxID string, full bool) (*nodeoperatorv1.GetSandboxDiagnosticsResponse, error)
	Close() error
}

var newDiagnosticsRPCClient = func(ctx *cli.Context) (diagnosticsRPCClient, error) {
	return client.New(ctx)
}

var DiagnosticsCmd = cli.Command{
	Name:  "diagnostics",
	Usage: "Print sandboxd diagnostics for a sandbox on the current node",
	Flags: []cli.Flag{
		cli.BoolFlag{Name: "full", Usage: "request full sandboxd diagnostics"},
		cli.BoolFlag{Name: "json", Usage: "print raw sandboxd diagnostics JSON"},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one sandbox id must be specified")
		}
		opsClient, err := newDiagnosticsRPCClient(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.GetSandboxDiagnostics(context.Args().First(), context.Bool("full") || context.Bool("json"))
		if err != nil {
			return err
		}
		if context.Bool("json") {
			fmt.Fprintln(os.Stdout, resp.GetRawJson())
			return nil
		}
		renderSandboxDiagnostics(os.Stdout, resp)
		return nil
	},
}
