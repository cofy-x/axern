# apps/cli Agent Contract

## Purpose

`apps/cli` is the Axern product CLI. It talks to the platform through product
APIs exposed through the gateway, and stays independent from
private node, runtime, network, and database internals.

Read this file and `apps/cli/README.md` before changing CLI behavior.

## Layering

- `internal/cliapp` is the composition root for the executable: global flags, command registration, and top-level app metadata.
- `internal/commands/<domain>` contains CLI adapters only: flags, argument validation, CLI-specific parsing, and output selection.
- `internal/application/<domain>` contains command use cases and cross-API orchestration.
- `internal/config` owns the local CLI config file and named context profiles.
- `internal/controlv1` owns control-plane configuration defaults, command context, mTLS dialing, and generated public API client construction.
- `internal/command` owns Cobra-neutral runtime options, context resolution, output validation, and exit semantics.
- `internal/output` owns terminal and JSON rendering.
- `internal/parse` owns pure flag/string parsing helpers.
- `internal/resourcespec` owns strict `axern/v1` YAML and JSON resource specs.
- `internal/workloaddiagnostic` owns shared workload diagnostic message classification used by application DTOs and output renderers.

Product command behavior should flow one way:

`cliapp -> commands -> application -> public SDK clients`

Commands should call application services instead of coordinating generated gRPC clients directly. Application packages may depend on generated public SDK clients and narrow local interfaces, but must not import command packages.

## API Boundaries

- Use public SDK packages under `sdk/go/gen/axern/control/...`.
- Dashboard reliability data must use the public admin RPC exposed by gatewayd.
- Do not import or depend on `control/controld/internal/...`, runtime-private APIs, node lifecycle APIs, database adapters, or implementation-only protos from this CLI.
- Keep private operational tooling out of `apps/cli` unless it is promoted to a product-facing workflow.

## Design Policy

- Axern is in active development. Prefer a coherent, maintainable shape over preserving flawed internal structure.
- Do not add transitional alias packages, compatibility shims, or workaround paths without an external contract that requires them.
- Keep command files small. If a workflow needs multiple API calls or non-trivial branching, put that orchestration in `internal/application/<domain>`.
- Keep rendering out of application services; return SDK response objects or small application result structs and let commands render them.
- Keep README examples representative. Do not turn `apps/cli/README.md` into a full command reference; the CLI help is the source of truth for exhaustive flags.

## Validation

Run these after CLI changes:

- `go test ./apps/cli/...`
- `go vet ./apps/cli/...`
- `make axern-cli-check-architecture`
- `test -z "$(gofmt -l apps/cli)"`
- `make axern-cli-build`

If generated SDK usage changes, also run the relevant SDK tests.
