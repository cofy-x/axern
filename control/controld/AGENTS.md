# AGENTS.md

## Purpose

This is the local agent contract for `control/controld`.

Use it to preserve the control-plane architecture while changing code. For
background, API inventory, and the full code-layout map, read
[Control Plane README](README.md).

## Architecture Contract

- `controld` is the Postgres-backed control-plane API and sandbox registry. It
  is not an invoke proxy, data-plane proxy, terminal streamer, or realtime exec
  path.
- Public user contracts belong in `sdk/proto`. The HTTP listener hosts
  diagnostics plus internal runtime artifact downloads; do not treat it as a
  user-facing product API or realtime data-plane proxy.
- Postgres is the only authoritative state backend. Do not add backend-pluggable
  storage, in-memory app profiles, or fallback state paths unless explicitly
  requested.
- Placement stays separate from node-internal execution. Realtime exec and
  terminal traffic still go directly to selected nodes or through the gateway
  data plane after resolution.
- Real provider probes belong exclusively to leased rollout workers. Controld
  persists Profile/credential snapshots, schedules work, enforces budgets, and
  stores typed results; it must not acquire provider HTTP clients.
- Profile-owned credential IDs remain private, and generic Secret get/list must
  never expose them. Rollout snapshot references fence rotation and retention.
- Controld issues artifact tickets and resolves them only on the private mTLS
  API after verifying the dedicated `gatewayd` certificate identity. It does
  not stream object bytes or return internal S3 URLs publicly.
- Managed provider usage is durable even when no explicit budget is configured.
  Completion usage must match its committed reservation, and rollout totals
  are derived from committed reservations so retried provider calls remain
  visible.

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
  project each affected service once per batch. Do not restore per-observation
  service transactions or process-local status state.
- Durable node lifecycle status is authoritative. Retired node identities must
  remain fenced from registration, node authentication, placement, and durable
  reservation admission; retirement must not become a transient retryable
  placement condition.
- Service reconciliation uses a bounded, service-ID-keyed event queue for
  latency. Autoscaling uses the fast periodic path; pending/retry recovery runs
  at startup and on the separate low-frequency safety sweep. Do not turn status
  events or the fast ticker back into full pending-service scans, or enqueue
  no-op syncs for state transitions already completed by durable projection.
  Reconcile independent services through the bounded worker pool. Dispatch node
  allocation creation with both global and per-node concurrency budgets, and
  preserve fair progress across nodes so service fanout and replica scale cannot
  multiply into unbounded RPCs or let one saturated node block the cluster.
- Rollout event watches and worker claim long-polls share one dedicated
  PostgreSQL `LISTEN` session per controld replica. Waiter concurrency must not
  consume the query pool. Work notifications represent only new candidate
  supply or released capacity: route supply to one compatible FIFO waiter per
  replica and bound capacity rescans to one waiter per capability group. Do not
  wake claimers for lease renewal or other non-actionable row updates.
  Notifications remain hints; event sequence and work claim state are durable
  and are always re-read from PostgreSQL with periodic jittered recovery.
- Do not add transitional alias bridges such as `type X = otherpkg.X` or
  `var X = otherpkg.X` when moving code.
- Do not create `internal/common` for convenience. Put domain rules in the
  owning kernel package and adapter plumbing in the adapter package.
- Keep SQL scan/marshal helpers in `internal/postgres/*` unless they are pure
  domain rules.
- Avoid catch-all files such as `helpers.go`, `utils.go`, or `interfaces.go`
  once a package grows. Prefer names that state the durable responsibility:
  `store.go`, `scan.go`, `tx.go`, `validation.go`, `rollout.go`,
  `autoscaling.go`, `allocation_contracts.go`, or `reconcile_contracts.go`.
- If behavior seems to belong partly in `internal/app`, put the behavior in the
  non-`app` package and let `app` assemble it.

## Feature Placement

- Service rollout, replacement, replica convergence, autoscaling, and service
  status: `application/service`, `kernel/service`, `postgres/service`.
- Run admission, cancellation, allocation cleanup, and internal execution lease issuance:
  `application/run`, `kernel/run`, `postgres/run`.
- Namespace lifecycle, namespace resource quota policy, and namespace lock
  rows: `postgres/namespace` for durable namespace state, quota policy, and
  locking; `kernel/resource` for pure quota evaluation; `postgres/reservation`
  for transactional reservation admission.
- Environment creation and template/image resolution:
  `application/environment`, `kernel/environment`, `postgres/run` for durable
  environment state.
- Function revisions, worker rollout, scaling, invocation history, and events:
  `application/function`, `kernel/function`, and `postgres/function`.
- Gateway route and terminal resolution:
  `application/gateway` plus `postgres/gateway` readers and lease issuers.
- Node reports, allocation status reports, inventory reconciliation,
  node-availability reconciliation, and lease watches:
  `api/nodev1`, `application/node`, `kernel/node`, `kernel/allocation`, and the
  owning durable stores.
- Allocation lifecycle retry admin operations, admin audit read models, and
  admin reliability read models: `api/adminv1`, `application/admin`,
  `kernel/allocation`, and `kernel/admin` plus `postgres/admin`; debug HTTP
  remains read-only and must not own retry writes or product CLI contracts.
- Background reconciler health rules and snapshots: `kernel/reconcile`, with
  process-local ownership in `internal/app` and read-only exposure through
  `api/debughttp`.
- Debug-only `*z` endpoints: `internal/api/debughttp`.
- Internal Function worker bundle downloads: `api/functionhttp`, with durable
  payload reads under `postgres/function`.
- Agent Profile and rollout lifecycle: `api/publicv1`, `kernel/agentprofile`,
  `kernel/rollout`, and their Postgres stores. Provider execution remains in
  Axrun workers.
- Artifact ticket resolution: private `api/artifactaccessv1` plus the rollout
  Postgres store; public byte streaming remains gatewayd-owned.

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
- If debug HTTP endpoints change, update `control/controld/README.md`.
- If package responsibilities or placement rules change, update this file and
  the Code Layout / Architecture sections in `control/controld/README.md`
  together.
