# SDK User Model

This document defines the stable user-facing boundary shared by Axern SDKs and
examples.

## Principles

- Keep the first runnable example short.
- Expose Axern concepts through product nouns: `Sandbox`, `Run`, and
  `Function`.
- Preserve the platform ownership model. SDK helpers should compile to public
  control-plane APIs instead of bypassing control-plane admission or node-local
  execution ownership.
- Keep low-level clients available for advanced workflows, but make the happy
  path obvious.

## Sandbox Files And Outputs

Sandbox writable files belong to one allocation. Reusable persistent volumes
are not part of the SDK contract; download required files or publish artifacts
explicitly before terminating the sandbox.

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_env()

with Sandbox(client=client, template_id="python311") as sandbox:
    sandbox.write_text("/tmp/result.txt", "hello from axern\n")
    sandbox.download_file("/tmp/result.txt", "result.txt", overwrite=False)
```

The downloaded file belongs to the caller's filesystem. It is not an automatic
object-store upload or a persistence guarantee for the sandbox directory.
Immutable image bundles and allocation-local TaskSet workspaces remain separate
runtime composition features. See the [storage lifetime contract](../architecture/storage-architecture.md)
for recovery and the historical-data upgrade boundary.

## Connections

SDK constructors require explicit endpoint and TLS configuration. Environment
and context loading are explicit factories:

```python
from axern_sdk import AxernClient

client = AxernClient.from_env()
hk = AxernClient.from_context("~/.config/axern/config.json", "hk")
```

`from_env()` reads:

- `AXERN_ENDPOINT`, defaulting to the public gateway API endpoint
  (`127.0.0.1:25000` in local dev)
- `AXERN_TLS_CA_CERT`
- `AXERN_TLS_CERT`
- `AXERN_TLS_KEY`

`from_context()` reads the same versioned context schema used by the CLI. SDK
constructors never inspect the user directory implicitly.

## Common Contract

The Go, Python, and TypeScript SDKs consume the same versioned fixtures under
`sdk/contracts/v1` for context and proxy behavior, resource quantities,
sandbox sources, lifecycle operations, files, processes, archives, tunnels,
and public error classification. Run `make sdk-contract-verify` before an SDK
release.

Public RPC errors preserve the operation, RPC code, server details,
retryability, and allocation identity when one exists. Validation, not found,
permission, timeout, cancellation, and unavailable failures remain distinct.
SDKs do not retry mutating RPCs. Idempotent reads and service-watch reconnects
may retry only within the caller's total deadline.

## Function

Function is the user-facing model for repeated event handling with a handler
contract, timeout, warm pool, autoscaling, and optional initializer.

The full product contract lives in
[Function User Model](./function-user-model.md). The short shape is:

```text
hello/
|-- function.yaml
|-- payload.json
`-- src/
    `-- handler.py
```

Resource spec:

```yaml
api_version: axern/v1
kind: Function
metadata:
  name: hello
  namespace: default
spec:
  source:
    template: python311
  function:
    runtime: python3.11
    handler: handler.hello
    initializer: handler.init
    source: src
    timeout_seconds: 600
    scaling:
      min_replicas: 0
      max_replicas: 10
      concurrency: 2
```

Python:

```python
from axern_sdk import AxernClient, Function

client = AxernClient.from_env()
fn = Function.from_dir(client, "./hello")

fn.deploy()
print(fn.invoke({"key": "axern"}))
```

Function deploy and invocation use the dedicated Function API. The worker
environment, revision, scaling state, invocation result, and invocation history
remain Function-owned semantics; they are not modeled as ordinary Run or
Service invocation helpers.
