# CLI Agent Contract

## Purpose

`apps/cli` is the public Axern CLI. Use the [CLI README](README.md) for commands.

## Ownership Boundaries

- Preserve the dependency direction `cliapp -> commands -> application -> public SDK clients`; commands must not coordinate generated clients directly.
- Resource commands use public SDK and gateway APIs. Do not import control-plane internals, runtime-private APIs, node lifecycle APIs, database adapters, or implementation-only protos.
- Host deployment commands have explicit local owners: `server` delegates to the verified installed native helper, while `dev` owns the Docker Compose development stack. They never infer a deployment backend from an API context, share a reset path, or duplicate the native lifecycle protocol.
- Expose the workload lifecycle defined by the [Stable Domain Model](../../docs/product/domain-model.md). SSH and Tunnel remain Allocation-bound workflows.
- Keep orchestration in application packages, rendering in output packages, and exhaustive flag reference in CLI help.

## Validation

Run `go test ./apps/cli/...`, `go vet ./apps/cli/...`, `make axern-cli-check-architecture`, and `make axern-cli-build`; use `make verify-changed` for SDK or integration effects.
