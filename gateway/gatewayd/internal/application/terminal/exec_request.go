package terminal

import (
	"strings"

	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func execStreamOpenRequest(resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) *nodesandboxv1.ExecStreamRequest {
	argv := append([]string(nil), opts.Argv...)
	tty := opts.TTY
	if len(argv) == 0 {
		argv = []string{"/bin/sh"}
		tty = true
	}
	return &nodesandboxv1.ExecStreamRequest{Payload: &nodesandboxv1.ExecStreamRequest_Open{Open: &nodesandboxv1.ExecStreamOpen{
		Spec: &nodesandboxv1.ExecSpec{
			Argv: argv,
			Tty:  tty,
			Env:  cloneEnv(opts.Env),
			User: strings.TrimSpace(opts.User),
		},
		AllocationID:        resolved.GetAllocationID(),
		Attempt:             resolved.GetAttempt(),
		ExecutionLeaseToken: resolved.GetLease().GetPlaintextToken(),
	}}}
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
