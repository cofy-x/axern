# CLI Agent Contract

## Purpose

`apps/cli` is the public Axern CLI. Read the [CLI README](README.md) for commands and package routing.

## Ownership Boundaries

- Keep `internal/cliapp` as the composition root, `internal/commands/<domain>` as CLI adapters, and `internal/application/<domain>` as command use cases and cross-API orchestration.
- Keep local context/configuration in `internal/config` and `internal/controlv1`, rendering in `internal/output`, pure parsing in `internal/parse`, and strict resource documents in `internal/resourcespec`.
- Preserve the dependency direction `cliapp -> commands -> application -> public SDK clients`; commands must not coordinate generated clients directly.
- Use public SDK and gateway APIs only. Do not import control-plane internals, runtime-private APIs, node lifecycle APIs, database adapters, or implementation-only protos.
- Expose the workload lifecycle defined by the [Stable Domain Model](../../docs/product/domain-model.md). SSH and Tunnel remain Allocation-bound workflows.
- Keep non-trivial branching in application packages and rendering in command/output packages. CLI help, not the README, is the exhaustive flag reference.

## Validation

- Run `go test ./apps/cli/...`, `go vet ./apps/cli/...`, `make axern-cli-check-architecture`, `test -z "$(gofmt -l apps/cli)"`, and `make axern-cli-build`.
- Run relevant SDK tests when generated client usage changes.
