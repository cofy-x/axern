# Axern Python SDK

The Axern Python SDK is the first-class programmable interface for Axern
sandboxes. It exposes both the control plane client and a high-level
`Sandbox` API for lifecycle, command execution, attached processes, file
operations, directory transfer, tunnels, capability discovery, and diagnostics.

## Install

From this repository:

```bash
uv build sdk/python
```

For local development, run examples and tests through the root `uv` workspace:

```bash
uv run --package axern-sdk python sdk/python/examples/sandbox_programming.py
```

## Connect

```python
from axern_sdk import AxernClient

client = AxernClient.from_context("~/.config/axern/config.json")
```

TLS-enabled local compose setups can pass certificate paths directly:

```python
client = AxernClient(
    "127.0.0.1:25000",
    tls_ca_cert=".dev/certs/ca.crt",
    tls_cert=".dev/certs/client.crt",
    tls_key=".dev/certs/client.key",
)
```

`AsyncAxernClient` provides the same control-plane surface for asyncio code.
Constructors are explicit and never read the user directory. `from_context()`
loads a named CLI context from the supplied path; `from_env()` is available for
environment-driven automation and reads `AXERN_ENDPOINT` plus the gateway TLS
and proxy variables.

## Sandbox Sources

Create a sandbox from exactly one source:

- `template_id="python311"` for a catalog template.
- `image="docker.io/library/python:3.12-slim"` for an OCI image.
- `environment_id="..."` for an existing environment.

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient("127.0.0.1:25000")

with Sandbox(client=client, template_id="python311") as sandbox:
    print(sandbox.metadata.allocation_id)

client.close()
```

## Volumes

Use `VolumeMount` to attach Service V1 volumes to service-backed sandboxes.
The SDK passes volume intent through the public control plane; storage
resolution, node publish, mount injection, and release remain owned by Axern's
Storage V1 runtime flow.

```python
from axern_sdk import Sandbox, VolumeMount

with Sandbox(
    client=client,
    template_id="python311",
    volumes=[
        VolumeMount("data", "/data"),
        VolumeMount("cache", "/cache", readonly=True, options=("rbind",)),
    ],
) as sandbox:
    result = sandbox.exec("ls /data /cache", text=True, check=True)
    print(result.stdout)
```

## Function Manifests

`Function.from_file()` loads and validates an `axern/v1` Function resource.
`Function.package()` creates a deterministic tar bundle, and `Function.deploy()`
packages the source, uploads it with `FunctionControl.UploadFunctionBundle`,
and then calls `FunctionControl.DeployFunction`. The `python311` runtime image
includes the SDK Function worker module used by controld-managed warm workers.
`Function.invoke()` calls the dedicated Function invocation API and returns a
decoded invocation result.

```python
from axern_sdk import AxernClient, Function

client = AxernClient.from_context("~/.config/axern/config.json")
function = Function.from_file(client, "examples/function-hello/function.yaml")
deployment = function.deploy(labels={"team": "runtime"})

print(function.name)
print(function.spec.handler)
print(deployment.function.id)
```

## Exec

Use `exec()` for command-result workflows. Set `text=True` to decode stdout and
stderr; set `check=True` to raise `SandboxExecError` on non-zero exit.

```python
with Sandbox(client=client, template_id="python311") as sandbox:
    result = sandbox.exec("python -c \"print('hello')\"", text=True, check=True)
    print(result.stdout)
```

Use `exec_stream()` when stdout/stderr should be consumed as events:

```python
for event in sandbox.exec_stream(["python", "-u", "-c", "print('streamed')"]):
    if event.stream == "stdout":
        print(event.text(), end="")
```

## Attached Process

Use `process()` when your program needs to control stdin, observe output, wait,
or terminate a running command.

```python
with sandbox.process(["python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"]) as process:
    process.write("hello process\n")
    process.close_stdin()

    for event in process.events():
        if event.stream == "stdout":
            print(event.text(), end="")

    result = process.wait()
    print(result.exit_code)
```

`AsyncSandbox.process()` returns `AsyncSandboxProcess` with async equivalents of
`write()`, `close_stdin()`, `events()`, `wait()`, `terminate()`, and `kill()`.

## Image-Backed Processes

Use `exec_image()` or `process_image()` to run a tool from a separate image
against explicit host-backed sandbox paths. OCI and Nydus image refs use the
same `image` field. When `mounts=None`, the SDK requests `/workspace ->
/workspace`; pass `mounts=[]` for no shared paths. Use `Sandbox(image=...)`
when the image should be the sandbox rootfs with normal files, exec, process,
tunnel, and lifecycle APIs; image-backed processes are temporary side processes
attached to an existing sandbox.

```python
from axern_sdk import workspace_mount

result = sandbox.exec_image(
    "ghcr.io/cofy-x/agent:latest",
    "tool run",
    mounts=[workspace_mount("/workspace")],
    check=True,
    text=True,
)
print(result.stdout)
```

## Files

Single-file APIs are byte-safe. Text helpers only encode/decode at the SDK
boundary.

```python
sandbox.write_text("/tmp/message.txt", "payload\n")
print(sandbox.read_text("/tmp/message.txt"))

sandbox.write_bytes("/tmp/blob.bin", b"\x00\x01")
data = sandbox.read_bytes("/tmp/blob.bin")
```

Platform file operations are handled by the node/runtime file service, not by
SDK-side shell fallbacks:

```python
sandbox.copy("/tmp/message.txt", "/tmp/message-copy.txt", overwrite=True)
sandbox.move("/tmp/message-copy.txt", "/tmp/message-final.txt")
sandbox.chmod("/tmp/message-final.txt", 0o600)
sandbox.touch("/tmp/message-final.txt")

info = sandbox.stat("/tmp/message-final.txt")
entries = sandbox.list_dir("/tmp")
exists = sandbox.exists("/tmp/message-final.txt")
```

## Directory Transfer

Directory upload/download uses archive streaming. The SDK packages local
directories with `tarfile` and safely extracts downloaded archives; remote file
semantics remain owned by the platform file service.

```python
from pathlib import Path

source = Path("example-upload")
source.mkdir(exist_ok=True)
source.joinpath("data.txt").write_text("directory payload\n")

sandbox.upload_dir(source, "/tmp/example-upload", overwrite=True)
sandbox.download_dir("/tmp/example-upload", "example-download", overwrite=True)
```

Local symlinks are rejected during upload. Download extraction rejects absolute
paths, parent traversal, symlinks, and hardlinks.

## Tunnel

Pass `upstream` to expose a local TCP service to code running inside the
sandbox. The SDK owns the tunnel connector and renews finite tunnel TTLs while
the sandbox is active.

```python
from axern_sdk import Sandbox

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    upstream="127.0.0.1:8080",
    remote_port=8786,
) as sandbox:
    print(sandbox.bound_addr)
```

## Metadata

`Sandbox.state` is the lightweight runtime state. `Sandbox.metadata` is stable
for logs and diagnostics:

```python
metadata = sandbox.metadata
print(metadata.environment_id, metadata.service_id, metadata.allocation_id)
print(metadata.node_id, metadata.runtime_class, metadata.tunnel_session_id)
```

## Capabilities

Use `capability_status()` to discover baseline and optional sandboxd-backed
providers before calling desktop or browser APIs:

```python
status = sandbox.capability_status()
print(status.ready, status.capabilities)

for provider in status.providers:
    print(provider.name, provider.state, provider.available, provider.reason)
```

## Errors

SDK exceptions expose fields for programmatic handling:

```python
from axern_sdk import (
    SandboxConnectionError,
    SandboxPermissionError,
    SandboxPreconditionError,
    SandboxRpcError,
)

try:
    sandbox.exec("python -V", check=True)
except SandboxConnectionError as exc:
    if exc.retryable:
        print("temporary node/control-plane connectivity issue")
    raise
except SandboxPermissionError as exc:
    print("credentials do not permit this operation", exc.operation)
    raise
except SandboxPreconditionError as exc:
    if exc.capability:
        print(
            exc.capability.capability,
            exc.capability.provider,
            exc.capability.provider_state,
            exc.capability.missing_dependencies,
        )
    raise
except SandboxRpcError as exc:
    print(exc.operation, exc.code, exc.details, exc.allocation_id)
    raise
```

Common error classes:

- `SandboxNotStartedError`: operation requires an active sandbox.
- `SandboxExecError`: `exec(..., check=True)` observed non-zero exit.
- `SandboxConnectionError`: transport or connectivity failure.
- `SandboxPermissionError`: authentication or authorization failure.
- `SandboxRpcError`: gRPC status mapped from node/runtime APIs.
- `SandboxTimeoutError`: SDK-side timeout.

Sandboxd-backed capability failures keep their normal SDK exception class and
also expose `exc.capability` when the node returns provider diagnostics. That
object contains `capability`, `provider`, `provider_state`, `reason`, and
`missing_dependencies`, so callers can branch on missing browser or
computer-use dependencies without parsing the full error string.

## Async

Async APIs mirror the synchronous shape:

```python
from axern_sdk import AsyncAxernClient, AsyncSandbox

async with AsyncAxernClient("127.0.0.1:25000") as client:
    async with AsyncSandbox(client=client, template_id="python311") as sandbox:
        result = await sandbox.exec("python -c \"print('hello async')\"", text=True, check=True)
        print(result.stdout)

        async with await sandbox.process(["python", "-u", "-c", "import sys; print(sys.stdin.read())"]) as process:
            await process.write("async input\n")
            await process.close_stdin()
            async for event in process.events():
                if event.stream == "stdout":
                    print(event.text(), end="")
```

## Examples

Runnable examples live in [`examples`](examples):

- [`examples/function_manifest.py`](examples/function_manifest.py)
- [`examples/sandbox_programming.py`](examples/sandbox_programming.py)
- [`examples/sandbox_volume.py`](examples/sandbox_volume.py)
- [`examples/async_sandbox_programming.py`](examples/async_sandbox_programming.py)
- [`examples/computer_use.py`](examples/computer_use.py)
- [`examples/service_gateway.py`](examples/service_gateway.py)

Examples expect a reachable Axern gateway control edge at `127.0.0.1:25000`.
`service_gateway.py` also expects `AXERN_SERVICE_URL`, for example
`http://127.0.0.1:25080`. It accepts `AXERN_NAMESPACE`, `AXERN_TEMPLATE_ID`,
`AXERN_RUNTIME_CLASS`, `AXERN_REQUEST_CPU`, `AXERN_REQUEST_MEMORY`,
`AXERN_LIMIT_CPU`, and `AXERN_LIMIT_MEMORY` for service configuration.

## Validation

```bash
make test-py
make lint-py
make sdk-python-verify
make local-compose-python-sdk-e2e
```
