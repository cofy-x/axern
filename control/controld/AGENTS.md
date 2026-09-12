# AGENTS.md

## Purpose

This is the local agent contract for `control/controld`.

Use it to preserve the control-plane architecture while changing code. For
background, API inventory, and the full code-layout map, read
[Control Plane README](README.md).

## Architecture Contract

- `controld` is the Postgres-backed owner of Environment, Run, Allocation,
  placement, lease, tunnel-session, and control-plane lifecycle state. It is
  not an invoke proxy, data-plane proxy, terminal streamer, or realtime exec
  path.
- Public user contracts belong in `sdk/proto`. The HTTP listener hosts
  diagnostics plus internal runtime artifact downloads; do not treat it as a
  user-facing product API or realtime data-plane proxy.
- Postgres is the only authoritative state backend. Do not add backend-pluggable
  storage, in-memory app profiles, or fallback state paths unless explicitly
  requested. During pre-stable convergence, update the initial schema directly
  and rebuild local databases; do not add old-schema detection, dual reads,
  aliases, or migration guards for retired internal models.
- The canonical execution chain is `Environment -> Run -> Allocation`.
  `Sandbox` is an SDK facade over this chain. Do not restore Service, Function,
  Agent Profile, or another parallel workload owner.
- Generic Volume Class, Claim, Binding, and physical reclaim are outside the
  execution core. Do not restore a storage daemon or a compatibility bridge.
  Preserve allocation-owned workspace/rootfs cleanup, ephemeral-storage
  limits, and artifact ownership.
- Placement stays separate from node-internal execution. Realtime exec and
  terminal traffic still go directly to selected nodes or through the gateway
  data plane after resolution.

## Layer Rules

- `internal/api/*`: gRPC/HTTP adapters only. Keep request validation,
  RPC-specific defaults, and response shaping here. Depend on narrow capability
  interfaces, not concrete Postgres stores.
- `internal/application/*`: use-case orchestration across kernel contracts,
  placement, nodebridge, and durable capabilities. Must not import
  `internal/postgres/*`.
- `internal/kernel/*`: domain contracts, parameter/result structs, pure rules,
  state transitions, and status calculations. Must not import `api`,
  `application`, `app`, or `postgres`.
- `internal/postgres/*`: SQL, `pgx` transactions, row scans, migration wiring,
  JSON persistence helpers, transactional reservation admission, and
  Postgres-specific error handling. It implements kernel/application
  capabilities; it does not own domain rules.
- `internal/app`: composition root only. Keep config, dependency construction,
  server registration, metric registration, lifecycle startup, and goroutine
  ownership here. Put workload workflows in `internal/application/*`.
- `internal/kernel/placement`: request-scoped candidate plans and pure
  placement preference ordering shared by selection and durable admission.
- `internal/placement`: placement request shaping, eligibility evaluation,
  candidate-plan construction, and selectors.
- `internal/nodebridge`: control-plane-to-node lifecycle request construction
  and RPC bridging.
- `internal/testutil/controldtest`: focused fakes and Postgres test harnesses.
  Keep test adapters out of production wiring.

## Design Rules

- Add explicit narrow capability interfaces for new behavior. Do not downcast
  from kernel/application interfaces to concrete Postgres types.
- Allocation status ingest is batch-oriented: authenticate the reporting node
  once, resolve allocation owners once, lock rows in deterministic order, and
  persist the affected Allocation and Run state once per batch. Do not restore
  per-observation transactions, a Service projection, or process-local status
  state.
- Durable node lifecycle status is authoritative. Retired node identities must
  remain fenced from registration, node authentication, placement, and durable
  reservation admission; retirement must not become a transient retryable
  placement condition.
- Allocation and capability reconciliation must use durable, bounded queues.
  Recovery may run at startup and through low-frequency safety sweeps; status
  events must not trigger full-table scans or no-op work already reflected in
  durable state. Dispatch node lifecycle RPCs with global and per-node
  concurrency budgets so one saturated node cannot block cluster progress.
- Do not add transitional alias bridges such as `type X = otherpkg.X` or
  `var X = otherpkg.X` when moving code.
- Do not create `internal/common` for convenience. Put domain rules in the
  owning kernel package and adapter plumbing in the adapter package.
- Keep SQL scan/marshal helpers in `internal/postgres/*` unless they are pure
  domain rules.
- Avoid catch-all files such as `helpers.go`, `utils.go`, or `interfaces.go`
  once a package grows. Prefer names that state the durable responsibility:
  `store.go`, `scan.go`, `tx.go`, `validation.go`, `admission.go`,
  `lifecycle.go`, or `reconcile.go`.
- If behavior seems to belong partly in `internal/app`, put the behavior in the
  non-`app` package and let `app` assemble it.

## Feature Placement

- Run admission, cancellation, status, allocation cleanup, and internal execution lease issuance:
  `application/run`, `kernel/run`, `postgres/run`.
- Namespace lifecycle, namespace resource quota policy, and namespace lock
  rows: `postgres/namespace` for durable namespace state, quota policy, and
  locking; `kernel/resource` for pure quota evaluation; `postgres/reservation`
  for transactional reservation admission.
- Environment creation and template/image resolution:
  `application/environment`, `kernel/environment`, `postgres/run` for durable
  environment state.
- Gateway allocation-target and terminal resolution:
  `application/gateway` plus `postgres/gateway` readers and lease issuers.
- Node reports, allocation status reports, inventory reconciliation,
  node-availability reconciliation, and lease watches:
  `api/nodev1`, `application/node`, `kernel/node`, `kernel/allocation`, and the
  owning durable stores.
- Observed capability transitions, allocation dependency conditions, and the
  separate capability reconcile queue: `application/capability`,
  `application/node`, `kernel/placement`, `kernel/allocation`, and
  `postgres/{nodes,allocation,admin}`. Platform provider ownership and loss
  policy remain in the shared `lib/go/nodecapability` catalog.
- Allocation lifecycle retry admin operations, admin audit read models, and
  admin reliability read models: `api/adminv1`, `application/admin`,
  `kernel/allocation`, and `kernel/admin` plus `postgres/admin`; debug HTTP
  remains read-only and must not own retry writes or product CLI contracts.
- Background reconciler health rules and snapshots: `kernel/reconcile`, with
  process-local ownership in `internal/app` and read-only exposure through
  `api/debughttp`.
- Debug-only `*z` endpoints: `internal/api/debughttp`.

## Validation

For ordinary `controld` Go changes, run:

```bash
make -C control/controld test
make -C control/controld vet
make -C control/controld check-architecture
test -z "$(gofmt -l control/controld)"
```

Also run `make agent-doc-check` when changing docs or this file.

For proto or wire-contract changes, also run:

```bash
make -C sdk/proto generate-go
```

and update generated SDK files plus docs that describe the API shape.

For placement, node report, node lifecycle, or axnoded-reporter integration
changes, run the `controld` checks above and the relevant `axnoded` regression
targets.

## Required Sync Points

- If `controld` ownership, path, or workspace membership changes, update
  `go.work`, root docs, and `.x/module-guide.md` together.
- If node summary shape or placement inputs change, update
  `.x/runtime-stack.md`, `docs/architecture/runtime-architecture.md`, `control/README.md`,
  `control/controld/README.md`, and `runtime/axnoded/README.md` together.
- If capability keys, evidence, requirement derivation, validity, transition,
  or loss reconciliation changes, update
  `docs/architecture/observed-capability-providers.md`, the shared catalog, and
  the axnoded contract together.
- If debug HTTP endpoints change, update `control/controld/README.md`.
- If package responsibilities or placement rules change, update this file and
  the Code Layout / Architecture sections in `control/controld/README.md`
  together.
