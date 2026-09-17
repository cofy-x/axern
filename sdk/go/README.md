# Axern Go SDK

The Go SDK is the second language surface for Axern programmable sandboxes. It focuses on the programmable sandbox loop:

- connect to the control plane
- create an allocation-backed sandbox from a template, image, or environment
- run a command
- inspect sandbox metadata for logs and diagnostics
- stream an attached process with stdin/stdout/stderr and lifecycle control
- discover sandbox capabilities before using optional providers
- read, write, inspect, mutate, and transfer sandbox files through platform file RPCs
- open a tunnel from the sandbox to a local upstream, with SDK-owned renewal and cleanup
- branch on typed error helpers such as `IsNotFound`, `IsTimeout`, and `IsValidation`
- close and clean up SDK-owned resources

The Go SDK follows the same platform boundaries: sandbox file, process, and tunnel behavior are delegated to Axern control/node/relay APIs instead of SDK shell fallbacks.

## Local Context

Add the published module to a Go project:

```bash
go get github.com/cofy-x/axern/sdk/go@latest
```

The public `sdk/go/clientconfig` package loads the same explicit context schema as the Axern CLI. Repository examples read the active CLI context, so `make axern-config-init` plus a running compose environment is enough for the examples and smoke target.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	axern "github.com/cofy-x/axern/sdk/go"
)

func main() {
	ctx := context.Background()
	client, err := axern.NewClient(ctx, "127.0.0.1:25000")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client: client,
		Image:  "docker.io/library/python:3.12-slim",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Close(ctx)

	if err := sandbox.Start(ctx); err != nil {
		log.Fatal(err)
	}

	result, err := sandbox.Exec(ctx, "python -c \"print('hello from go')\"", axern.ExecOptions{Check: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(result.StdoutString())

	if err := sandbox.WriteFile(ctx, "/tmp/message.txt", []byte("payload\n"), axern.WriteFileOptions{CreateParents: true}); err != nil {
		log.Fatal(err)
	}
	info, err := sandbox.Stat(ctx, "/tmp/message.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s has %d bytes\n", info.Path, info.Size)

	data, err := sandbox.ReadFile(ctx, "/tmp/message.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(data))

	if err := sandbox.Copy(ctx, "/tmp/message.txt", "/tmp/message.copy", axern.CopyOptions{Overwrite: true}); err != nil {
		log.Fatal(err)
	}

	process, err := sandbox.Process(ctx, []string{"python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"}, axern.ProcessOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if err := process.WriteString("streamed input\n"); err != nil {
		log.Fatal(err)
	}
	if err := process.CloseStdin(); err != nil {
		log.Fatal(err)
	}
	output, err := process.Output()
	if err != nil {
		log.Fatal(err)
	}
	if output.ExitCode != 0 {
		log.Fatalf("process exited with %d: %s", output.ExitCode, output.Message)
	}
	fmt.Print(string(output.Stdout))

	capabilities, err := sandbox.CapabilityStatus(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(capabilities.Capabilities)
}
```

## Network Policies

Leaving `SandboxOptions.NetworkPolicy` nil requests unrestricted networking. Strict policies are fail-closed; DNS deny only refuses matching traditional UDP/TCP DNS queries and does not block direct IP traffic, DoH, DoT, or already-resolved addresses.

```go
policy, err := axern.DenyDNSNetworkPolicy(
	"github.com",
	"*.github.com",
	"githubusercontent.com",
	"*.githubusercontent.com",
)
if err != nil {
	log.Fatal(err)
}
sandbox, err := axern.NewSandbox(axern.SandboxOptions{
	Client:        client,
	Image:         "docker.io/library/python:3.12-slim",
	NetworkPolicy: policy,
})
```

`AllowDomainNetworkPolicy` allows only strict HTTP/HTTPS destinations validated by DNS plus HTTP Host or TLS SNI. `NewStrictNetworkPolicy` also accepts explicit TCP/UDP CIDR and port grants; `DenyAllNetworkPolicy` allows no egress.

Mount a reusable read-only image bundle into the primary sandbox with `SandboxOptions.ImageMounts` when the task image should remain the rootfs and the mounted image only contributes files:

```go
sandbox, err := axern.NewSandbox(axern.SandboxOptions{
	Image: "registry.example.com/task-image:latest",
	ImageMounts: []axern.ImageMount{{
		Image:  "registry.example.com/tool-bundle:latest",
		Target: "/opt/axern/tools/example",
	}},
})
```

## Examples

Runnable examples live under `sdk/go/examples`:

- `basic`: start a sandbox and run a command
- `process`: stream stdin/stdout with an attached process
- `files`: use file and archive APIs
- `tunnel`: expose a local upstream to the sandbox through tunnel mode
- `computer-use`: inspect a desktop-capable sandbox and capture a screenshot
- `programmable`: combine upload, process control, download, tunnel, and cleanup

Each example defaults to the current Axern context from the local CLI config file. Use `--context` or `AXERN_CONTEXT` to select a named context, and `--config` or `AXERN_CONFIG` to select a config file. Explicit flags and env vars still take precedence, including `AXERN_ENDPOINT`, `AXERN_TLS_CA_CERT`, `AXERN_TLS_CERT`, `AXERN_TLS_KEY`, `AXERN_TEMPLATE_ID`, `AXERN_PROXY_MODE`, and `AXERN_TLS_SERVER_NAME`. Tunnel traffic reuses the gateway endpoint and TLS identity.

```bash
go run ./sdk/go/examples/basic
go run ./sdk/go/examples/process
go run ./sdk/go/examples/files
go run ./sdk/go/examples/tunnel
go run ./sdk/go/examples/computer-use --template-id desktop-base
go run ./sdk/go/examples/programmable
```

The lightweight examples smoke target runs `basic`, `process`, and `files` against the local compose context:

```bash
make sdk-go-examples-smoke
```

## Usage Notes

- Prefer `defer sandbox.Close(ctx)` for SDK-owned sandboxes. Closing a sandbox also closes SDK-owned tunnels and attached processes.
- Use `process.Output()` when you want collected stdout, stderr, and exit status. Use `process.Events()` or `process.Recv()` when output should be handled incrementally.
- Use `client.NodeSandbox(allocationID)` when you already have an allocation ID and want the lower-level file/process/exec API without creating a new SDK-owned sandbox.
- Use `sandbox.CapabilityStatus(ctx)` to discover baseline and optional provider availability before calling Computer Use APIs.
- Use `ExecOptions{Check: true}` for command-style failures that should return `ExecError`.
- Branch on helpers such as `IsNotFound`, `IsTimeout`, `IsUnavailable`, and `IsValidation` instead of parsing error text.
- Sandboxd-backed capability failures remain `RPCError` values. When provider diagnostics are present, `RPCError.Capability` contains structured capability, provider, provider state, reason, and missing dependency details.
- Use tunnels when a sandbox must reach a caller-local upstream such as a mock HTTP service or development server. Provider credentials and agent-specific configuration are owned by the external runner or another caller, not by the Axern sandbox lifecycle.

```go
result, err := sandbox.Exec(ctx, axern.Args("python", "-c", "import sys; sys.exit(7)"), axern.ExecOptions{Check: true})
var execErr *axern.ExecError
if errors.As(err, &execErr) {
	fmt.Println("exit", execErr.ExitCode(), result.StderrString())
}

_, statErr := sandbox.Stat(ctx, "/tmp/missing")
if axern.IsNotFound(statErr) {
	fmt.Println("missing")
}

var rpcErr *axern.RPCError
if errors.As(statErr, &rpcErr) && rpcErr.Capability != nil {
	fmt.Println(rpcErr.Capability.Capability, rpcErr.Capability.MissingDependencies)
}
```

## Validation

```bash
make sdk-go-verify
make sdk-go-examples-smoke
```

With local compose running, `make local-compose-go-sdk-e2e` verifies real sandbox exec, process, files, archives, and tunnels through one Allocation-scoped lifecycle.

## Declared Output

Add `DeclaredOutputs` to `SandboxOptions` or `ExecutionConfig` before Run creation. After the Run is terminal, call `GetSealedOutputManifest(ctx, runID)` and `DownloadSealedOutput(ctx, runID, outputID, offset, writer)`. A zero-offset download verifies the complete size and SHA-256 digest. The sealed bytes are node-local for 15 minutes after cleanup starts, survive axnoded restart but not Node-disk loss, and must be copied into caller-owned durable storage. Limits are 16 paths, 64 MiB per file, 256 MiB per tar, and 256 MiB total. This does not create a persistent workspace or object store.
