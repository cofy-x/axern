# Axern Python SDK

The Axern Python SDK is the first-class programmable interface for Axern sandboxes. It exposes both the control plane client and a high-level `Sandbox` API for lifecycle, command execution, attached processes, file operations, directory transfer, tunnels, capability discovery, and diagnostics.

## Install

Install the published package:

```bash
uv add axern-sdk
```

For repository development, build and run examples through the root `uv` workspace:

```bash
uv build --no-sources sdk/python
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

`AsyncAxernClient` provides the same control-plane surface for asyncio code. Constructors are explicit and never read the user directory. `from_context()` loads a named CLI context from the supplied path; `from_env()` is available for environment-driven automation and reads `AXERN_ENDPOINT` plus the gateway TLS and proxy variables.

## Sandbox Sources

Create a sandbox from exactly one source:

- `image="docker.io/library/python:3.12-slim"` for an OCI image.
- `template_id="python311"` for a deployment-provided template input.
- `environment_id="..."` for an existing environment.

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient("127.0.0.1:25000")

with Sandbox(client=client, image="docker.io/library/python:3.12-slim") as sandbox:
    print(sandbox.metadata.allocation_id)

client.close()
```

## Network Policies

Omitting `network_policy` requests unrestricted networking. Strict policies are fail-closed; `deny_dns` only refuses matching traditional UDP/TCP DNS queries and does not block direct IP traffic, DoH, DoT, or already-resolved addresses.

```python
from axern_sdk import NetworkPolicy, Sandbox

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    network_policy=NetworkPolicy.deny_dns(
        "github.com",
        "*.github.com",
        "githubusercontent.com",
        "*.githubusercontent.com",
        "gitlab.com",
        "*.gitlab.com",
        "bitbucket.org",
        "*.bitbucket.org",
    ),
) as sandbox:
    print(sandbox.metadata.allocation_id)
```

`NetworkPolicy.allow_domains("example.com", "*.example.com")` is strict: only HTTP/HTTPS traffic whose controlled DNS result and HTTP Host or TLS SNI match is allowed. Use `NetworkPolicy.strict(..., cidr_rules=(CIDRRule(...),))` for explicit TCP/UDP CIDR and port grants, and `NetworkPolicy.deny_all()` for no egress.

## Allocation-Local Files

Writable files belong to the sandbox allocation and are not preserved across replacement or node loss. Use the file APIs while the sandbox is alive and explicitly download or publish outputs that must outlive it.

```python
from axern_sdk import Sandbox

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
) as sandbox:
    sandbox.write_text("/tmp/result.txt", "allocation-local output\n")
    sandbox.download_file("/tmp/result.txt", "result.txt")
```

## Exec

Use `exec()` for command-result workflows. Set `text=True` to decode stdout and stderr; set `check=True` to raise `SandboxExecError` on non-zero exit.

```python
with Sandbox(client=client, image="docker.io/library/python:3.12-slim") as sandbox:
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

Use `process()` when your program needs to control stdin, observe output, wait, or terminate a running command.

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

`AsyncSandbox.process()` returns `AsyncSandboxProcess` with async equivalents of `write()`, `close_stdin()`, `events()`, `wait()`, `terminate()`, and `kill()`.

## Files

Single-file APIs are byte-safe. Text helpers only encode/decode at the SDK boundary.

```python
sandbox.write_text("/tmp/message.txt", "payload\n")
print(sandbox.read_text("/tmp/message.txt"))

sandbox.write_bytes("/tmp/blob.bin", b"\x00\x01")
data = sandbox.read_bytes("/tmp/blob.bin")
```

Platform file operations are handled by the node/runtime file service, not by SDK-side shell fallbacks:

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

Directory upload/download uses archive streaming. The SDK packages local directories with `tarfile` and safely extracts downloaded archives; remote file semantics remain owned by the platform file service.

```python
from pathlib import Path

source = Path("example-upload")
source.mkdir(exist_ok=True)
source.joinpath("data.txt").write_text("directory payload\n")

sandbox.upload_dir(source, "/tmp/example-upload", overwrite=True)
sandbox.download_dir("/tmp/example-upload", "example-download", overwrite=True)
```

Local symlinks are rejected during upload. Download extraction rejects absolute paths, parent traversal, symlinks, and hardlinks.

## Tunnel

Pass `upstream` to expose a local TCP service to code running inside the sandbox. The SDK owns the tunnel connector and renews finite tunnel TTLs while the sandbox is active.

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

For a lower-level runner flow, create the Run, wait for its public `allocation_id`, create a TunnelSession, then construct `TunnelConnector(client=client, ...)`. The connector inherits the Axern mTLS transport from the client; callers never import a private transport type. `wait_closed()` provides bounded cleanup/revocation observation. Tunnel authority is finite, does not renew the Run or ExecutionLease, and is revalidated online within 20 seconds (15-second interval plus a 5-second validation deadline).

For model access, keep the Provider API key, client certificate, routing and budget in a runner-local loopback gateway and tunnel raw TCP to it. Never copy the Provider identity, Axern mTLS key or Tunnel client token into the sandbox, Run metadata, declared output or CandidateBundle.

## Metadata

`Sandbox.state` is the lightweight runtime state. `Sandbox.metadata` is stable for logs and diagnostics:

```python
metadata = sandbox.metadata
print(metadata.environment_id, metadata.run_id, metadata.allocation_id)
print(metadata.run_id, metadata.tunnel_session_id)
```

## Capabilities

Use `capability_status()` to discover baseline and optional sandboxd-backed providers before calling Computer Use APIs:

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

Sandboxd-backed capability failures keep their normal SDK exception class and also expose `exc.capability` when the node returns provider diagnostics. That object contains `capability`, `provider`, `provider_state`, `reason`, and `missing_dependencies`, so callers can branch on missing Computer Use dependencies without parsing the full error string.

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

- [`examples/sandbox_programming.py`](examples/sandbox_programming.py)
- [`examples/async_sandbox_programming.py`](examples/async_sandbox_programming.py)
- [`examples/computer_use.py`](examples/computer_use.py)

Examples expect a reachable Axern gateway control edge at `127.0.0.1:25000`.

## Validation

```bash
make test-py
make lint-py
make sdk-python-verify
make local-compose-python-sdk-e2e
```

## Read-only Image Mounts And Secret Projections

Image mounts and Secret projections are immutable Run inputs. Image mounts are always read-only and therefore expose only an image reference and target path. Secret inputs carry references (`secret_id` and `key`), never plaintext values. They belong to the Run that declares them and are not inherited by a later verification Run.

```python
from axern_sdk import ImageMount, Sandbox, SecretFile

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    image_mounts=[
        ImageMount(
            "registry.example/claude-code@sha256:<digest>",
            "/__claude_code",
        )
    ],
    secret_files=[
        SecretFile(
            "/run/secrets/workload-config",
            "secret-workload",
            "settings.json",
            mode=0o400,
        )
    ],
) as sandbox:
    sandbox.exec(["/__claude_code/bin/claude"], check=True)
```

Targets, environment names, duplicate projections, file modes, and Secret namespace ownership are validated by the control plane. Secret files default to `0400`; explicit modes must contain no write bits, and pseudo-filesystems, executable/library trees, Axern's runtime state, and critical host-identity files are protected targets. Optional projections may be absent, but an existing Secret in another Namespace is never eligible.

Secret projection is for a least-privilege workload credential that must exist inside that Run. It is not the recommended Provider-credential path for an external agent runner; use the Tunnel-backed runner-local model gateway described above.

## Declared And Stream Output

Declare bounded result paths when creating a Run or Sandbox. Axern quiesces the Allocation and seals each regular file or directory-as-tar before runtime cleanup. Query the manifest and download by the persisted `run_id`; full downloads verify the declared size and SHA-256 digest.

```python
from axern_sdk import DeclaredOutput, DeclaredOutputFormat, Sandbox

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    declared_outputs=[DeclaredOutput("/tmp/result.json", DeclaredOutputFormat.FILE, "application/json")],
) as sandbox:
    sandbox.write_text("/tmp/result.json", '{"ok":true}\n')
    run_id = sandbox.metadata.run_id

run = client.wait_run(run_id, timeout=60)
output = next(item for item in client.get_sealed_output_manifest(run_id) if item.path == "/tmp/result.json")
with open("result.json", "wb") as destination:
    client.download_sealed_output(run_id, output.output_id, destination)
```

Declared output is retained on the Node for 15 minutes after cleanup starts. It survives axnoded restart, but not Node-disk loss, and must be copied to caller-owned durable storage. Limits are 16 paths, 64 MiB per file, 256 MiB per tar archive, and 256 MiB total. Missing, unsafe, wrong-kind, oversized, capture-failed, and node-unavailable results are explicit manifest states. This is not a persistent workspace, Artifact service, or object store.

stdout/stderr use the same node-local retention lifecycle and have a combined 64 MiB readable limit. Python `exec()` collects at most 1 MiB per stream while continuing to drain the RPC and reports truncation; use `exec_stream()` or `process()` for larger output. `Sandbox.close()` requests Run cancellation and reports cleanup failures, but does not wait for the durable terminal state; call `wait_run()` explicitly when terminal confirmation matters.
