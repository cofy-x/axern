package terminal

import (
	"strings"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func processOpenRequest(resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) *nodesandboxv1.ProcessRequest {
	argv := append([]string(nil), opts.Argv...)
	tty := opts.TTY
	if len(argv) == 0 {
		argv = []string{"/bin/sh"}
		tty = true
	}
	return &nodesandboxv1.ProcessRequest{Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
		Spec: &nodesandboxv1.ExecSpec{
			Argv: argv,
			Tty:  tty,
			Env:  cloneEnv(opts.Env),
			User: strings.TrimSpace(opts.User),
		},
		AllocationID: resolved.GetAllocationID(),
		InitialSize:  initialTerminalSize(opts),
	}}}
}

func initialTerminalSize(opts OpenOptions) *nodesandboxv1.TerminalResize {
	if opts.InitialCols == 0 || opts.InitialRows == 0 {
		return nil
	}
	return &nodesandboxv1.TerminalResize{Cols: opts.InitialCols, Rows: opts.InitialRows}
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = value
	}
	return out
}
