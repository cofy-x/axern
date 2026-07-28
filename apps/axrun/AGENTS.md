# apps/axrun Agent Contract

## Purpose

`apps/axrun` is the Axrun product CLI. Axrun is Axern's native agent harness,
task runner, verifier, and trajectory capture CLI. It consumes Axern public
SDK/API surfaces and must stay layered above Axern core runtime services.

Read [Axrun README](README.md) for commands and choose additional context by
task:

| Change | Read |
| :--- | :--- |
| Package ownership, execution flow, or backend boundary | [Architecture](docs/architecture.md) |
| Native records, schemas, run directories, or exports | [Domain Model](docs/domain-model.md) |
| Rollout phases, SSE, errors, or artifact evidence | [Rollout Evidence](docs/rollout-evidence.md) |
| CLI flags or command workflow | [Usage](docs/usage.md) |
| Acceptance cases or verification scope | [Acceptance Matrix](docs/acceptance.md) |

Do not read all Axrun documents for a contained change.

## Layering

- `internal/cliapp` is the executable composition root.
- `internal/commands/<domain>` contains CLI adapters only: flags, argument
  validation, command-specific parsing, and output selection.
- `internal/application/<domain>` orchestrates rollout, TaskSet lifecycle,
  validation, export, and HTTP server workflows.

Command behavior flows one way:

```text
cliapp -> commands -> application -> rollout/taskset/localstore/domain
                                     -> sandbox/agent/backend
                                     -> Axern public SDK/API (when runner=axern)
```

## Package Ownership

- `internal/domain`: native data model and pure summaries.
- `internal/contract`: shared semantic invariants used by TaskSet/schema/store,
  agent runtime selection, and export gating.
- `internal/schema`: run-directory validation and cross-file contract checks.
- `internal/taskset`: strict `axrun/v1` build parsing, deterministic compilation,
  local/OCI resolution, and local/Kova publishing.
- `internal/rollout`: episode lifecycle engine, agent/verifier flow, workspace
  capture, trajectory events, and sidecar writes.
- `internal/application/*`: command-level orchestration over native services,
  including task preparation, rollout, validation, export, and the local
  HTTP server.
- `internal/backend/*`: thin backend-specific adapter wiring over shared rollout.
- `internal/sandbox/*`: local and Axern sandbox abstractions used by backend
  adapters.
- `internal/agent/*`: agent harnesses, launch plans, profiles, and
  agent-specific runtime metadata.
- `internal/runtimeimage`: task runtime image build/import helpers for
  `dockerfile` runtime sources.
- `internal/reward` and `internal/verifier`: native reward normalization and
  verifier result shaping.
- `internal/localstore` and `internal/runref`: run-directory persistence and
  portable run-root-relative refs.
- `internal/proxy`: model proxy capture, recorder, and usage/cost helpers.

## API Boundaries

- Use public Axern SDK/API surfaces only.
- Do not import `control/*/internal`, runtime-private APIs, node lifecycle
  APIs, database adapters, or implementation-only protos.
- Do not add Axern proto or database schemas from this app. Promote stable
  Axrun concepts into platform APIs only after local rollout execution
  validates the model.
- Do not import external-format parsing logic into rollout execution. External
  integrations should produce `axrun/v1` `TaskSetBuild` specifications before
  invoking the deterministic compiler.
- Do not make Axrun a benchmark runner, seed-generation product, or agent
  implementation framework. Those systems should call Axrun as an atomic
  execution, verification, and trajectory-capture capability.
- Keep the HTTP server as a thin application surface over rollout services.
  It may expose bounded local rollout execution, SSE completion events, and run
  status reads, but it must not become a separate execution engine.
- Keep SSE phase events, rollout error codes, artifact manifests, and diagnosis
  evidence aligned with `apps/axrun/docs/rollout-evidence.md`.
- Managed provider probes run in the leased Axrun worker network. Do not move
  provider clients into controld; it owns state, scheduling, frozen snapshots,
  metering admission, and typed result persistence.
- Public artifact download is gatewayd mTLS gRPC streaming. Axrun must not
  expose or consume internal S3 URLs, object-store credentials, or tickets in
  logs and stable output.
- Every managed provider probe and episode is durably metered, including
  rollouts without an explicit budget. Upload evidence before committing
  episode usage, and send the committed reservation ID with completion.

## Design Policy

- Keep Axrun separate from the `axern` CLI. It is a sibling product
  entrypoint, not an `axern` subcommand.
- Keep execution compiled-only. Rollout execution consumes a frozen TaskSet
  descriptor and native `TaskInstance` records only.
- Keep agent implementations pluggable. Axrun may provide adapters for agent
  tools such as Claude Code, Codex, custom commands, and mounted agent bundles,
  but agent behavior belongs in those tools or adapters rather than in the core
  rollout engine.
- Keep task environment images and agent bundle images separate:
  `TaskInstance.sandbox.runtime_source` defines the sandbox rootfs, while
  `AgentSpec.runtime.image` defines a read-only agent/tool bundle mounted into
  that sandbox by Axern.
- Prefer explicit domain objects over compatibility shims while the product is
  still in active development.

## Validation

Run these after Axrun changes:

```bash
go test ./apps/axrun/...
go vet ./apps/axrun/...
test -z "$(gofmt -l apps/axrun)"
make axrun-local-smoke
make axrun-managed-rollout-compose-e2e
make agent-doc-check
```

For docs-only changes, `make agent-doc-check` and
`make axrun-local-smoke` are the minimum gate.

For release-level Axrun changes, also run:

```bash
make axrun-verify
bash ./scripts/verify-all.sh --include-axrun --from axrun-verify --to axrun-verify
```
