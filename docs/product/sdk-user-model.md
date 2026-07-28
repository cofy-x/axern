# SDK User Model

This document defines the stable user-facing boundary shared by Axern SDKs and
examples.

## Principles

- Keep the first runnable example short.
- Expose Axern concepts through product nouns: `Sandbox`, `VolumeMount`, and
  `Function`.
- Preserve the platform ownership model. SDK helpers should compile to public
  control-plane APIs instead of bypassing `controld`, `storaged`, `axnoded`, or
  `volumed`.
- Keep low-level clients available for advanced workflows, but make the happy
  path obvious.

## Sandbox Volumes

Sandboxes are service-backed, so they can use Service V1 volume mounts. The
Python SDK should expose that with a small object rather than making users build
protobuf messages.

```python
from axern_sdk import AxernClient, Sandbox, VolumeMount

client = AxernClient.from_env()

with Sandbox(
    client=client,
    template_id="python311",
    volumes=[
        VolumeMount("data", "/data"),
        VolumeMount("cache", "/cache", readonly=True),
    ],
) as sandbox:
    result = sandbox.exec("ls /data /cache", text=True, check=True)
    print(result.stdout)
```

`VolumeMount` is user intent. Storage placement, node publish, runtime mount
injection, and release continue to flow through the existing
`storaged -> controld -> axnoded -> volumed` chain.

The first supported source is the Storage V1 local provider truth path. Object
store datasets, NFS, PVCs, and image-backed data mounts should arrive as
storage providers when their product contracts are concrete, not as SDK-only
shortcuts.

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
