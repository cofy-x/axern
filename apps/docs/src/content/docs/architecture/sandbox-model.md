---
title: Sandbox Model
description: The shared mental model behind every SDK Sandbox — service-backed lifecycle, execution, files, and explicit cleanup.
---

A Sandbox is the SDK's programmable view of an isolated workload. The same
model is implemented by the Python, Go, and TypeScript SDKs against shared
versioned contracts, so concepts transfer between languages.

## A Sandbox is service-backed

Constructing and starting a Sandbox compiles to public control-plane APIs, not
a private channel:

1. Resolve or create an **Environment** (image, catalog template, or existing
   environment ID — exactly one source).
2. Create a **Service** with one replica and wait for a ready allocation.
3. Execute, transfer files, and open tunnels through the node data plane.

Because the backing resources are ordinary Axern resources, they remain
visible to the CLI (`axern service list`) and subject to the same namespace
quota, admission, and authorization rules as any other workload.

## Sources and connections

Each Sandbox selects exactly one source: a portable OCI `image`, a catalog
`template_id`, or an existing `environment_id` to continue prior work.

Connections are explicit. `AxernClient.from_env()` reads `AXERN_ENDPOINT` and
`AXERN_TLS_*` variables; `from_context()` reads the same versioned context
schema as the CLI. SDK constructors never inspect the user directory
implicitly.

## Execution and files

Once started, a Sandbox supports:

- `exec()` for one-shot commands with exit-code and output capture, and
  `process()` for interactive stdin, termination, and explicit waits
- file operations (`read_text`/`write_text`, `list_dir`, `stat`, `mkdir`,
  `remove`, `copy`, `move`, `chmod`) plus archive-backed
  `upload_dir`/`download_dir` that reject unsafe paths and links
- reverse [tunnels](/guides/tunnels/) with SDK-owned renewal and cleanup
- [computer use and browser automation](/guides/computer-use/) on capable
  images
- persistent [volumes](/guides/storage/) mounted at creation time

## Lifecycle and cleanup

`close()` is deliberate and ordered: stop tunnel renewal, revoke tunnel
sessions, delete the SDK-created Service (which releases the allocation), and
delete the Environment only when the SDK created it — an Environment passed in
by ID is never deleted. All cleanup is best-effort; prefer context managers or
`defer` so cleanup runs on every path.

Sandbox timeouts govern readiness and RPC deadlines, not lifetime. A Sandbox
has no built-in idle expiry; its allocation lives until `close()` or an
operator action.

## Errors

Public RPC errors preserve the operation, RPC code, server details,
retryability, and allocation identity. Validation, not-found, permission,
timeout, cancellation, and unavailable failures stay distinct. SDKs never
retry mutating RPCs implicitly; idempotent reads and service-watch reconnects
retry only within the caller's deadline.

The authoritative contract is the repository's
[SDK user model](https://github.com/cofy-x/axern/blob/main/docs/product/sdk-user-model.md).
