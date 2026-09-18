# SDK User Model

This document defines the stable user-facing boundary shared by Axern SDKs and examples. Object identity, persistence, and ownership follow the [Stable Domain Model](domain-model.md).

## Principles

- Keep the first runnable example short.
- Expose Axern concepts through the small product vocabulary of `Environment`, `Run`, `Sandbox`, and `Tunnel`.
- Treat `Environment -> Run -> Allocation` as the only durable execution model. `Sandbox` owns SDK ergonomics and cleanup around that chain; it is not a separately persisted resource or a hidden Service.
- Preserve the platform ownership model. SDK helpers should compile to public control-plane APIs instead of bypassing control-plane admission or node-local execution ownership.
- Keep low-level clients available for advanced workflows, but make the happy path obvious.

## Sandbox Files And Outputs

Sandbox writable files, retained stdout/stderr, and bounded declared outputs belong to one Allocation. Reusable persistent volumes and durable output objects are not part of the SDK contract. A caller can declare up to 16 regular-file or directory-as-tar paths in the immutable Run specification. The node quiesces the Allocation and atomically publishes their manifest before deleting the runtime. Run stdout/stderr and declared outputs remain readable for 15 minutes from cleanup initiation, subject to node-disk availability. An upper-layer system owns durable publication of downloaded bytes.

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_env()

with Sandbox(client=client, template_id="python311") as sandbox:
    sandbox.write_text("/tmp/result.txt", "hello from axern\n")
    sandbox.download_file("/tmp/result.txt", "result.txt", overwrite=False)
```

For crash recovery between process completion and client download, declare the path at Run creation and retrieve it later by persisted `run_id`. Full SDK downloads verify the manifest size and SHA-256 digest. The downloaded file belongs to the caller's filesystem; it is not an automatic object-store upload or a persistence guarantee for the Sandbox directory. Immutable image inputs and Allocation-local writable workspaces remain separate runtime concerns. See the [storage lifetime contract](../architecture/storage-architecture.md).

## Connections

SDK constructors require explicit endpoint and TLS configuration. Environment and context loading are explicit factories:

```python
from axern_sdk import AxernClient

client = AxernClient.from_env()
hk = AxernClient.from_context("~/.config/axern/config.json", "hk")
```

`from_env()` reads:

- `AXERN_ENDPOINT`, defaulting to the public gateway API endpoint (`127.0.0.1:25000` in local dev)
- `AXERN_TLS_CA_CERT`
- `AXERN_TLS_CERT`
- `AXERN_TLS_KEY`

`from_context()` reads the same versioned context schema used by the CLI. SDK constructors never inspect the user directory implicitly.

## Common Contract

The Go, Python, and TypeScript SDKs consume the same versioned fixtures under `sdk/contracts/v1` for context and proxy behavior, resource quantities, sandbox sources, lifecycle operations, files, processes, archives, tunnels, and public error classification. Run `make sdk-contract-verify` before an SDK release.

Public RPC errors preserve the operation, RPC code, server details, retryability, and allocation identity when one exists. Validation, not found, permission, timeout, cancellation, and unavailable failures remain distinct. SDKs do not retry mutating RPCs. Idempotent reads and Run-watch reconnects may retry only within the caller's total deadline.
