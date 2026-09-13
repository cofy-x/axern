# controld

`controld` is Axern's durable control-plane authority for catalog, environment, namespace, Run, gateway, tunnel, and secret APIs. External CLI and SDK traffic should enter through `gatewayd`'s control edge; `controld` stays on the private control-plane network. It is backed by Postgres, which is the authoritative state store for control plane and node state.

`controld` owns:

- node registration, heartbeat, summary, active inventory ingest, node-availability reconciliation, and audited irreversible node retirement
- authenticated allocation lifecycle batch ingest with durable Run projection
- Environment and Run lifecycle control
- allocation admission, node reservations, execution leases, and tunnel sessions
- namespace lifecycle, resource quota policy, quota admission, and quota usage reporting
- allocation terminal and tunnel relay target resolution
- controld-managed secret metadata, encryption, and resolution
- read-only runtime catalog and debug HTTP surfaces

`controld` does not own realtime exec or terminal streaming. Realtime execution goes to selected nodes through the current SDK path, and `gatewayd` owns external control/data-plane forwarding after resolving routes here.

Run creation persists desired execution state and returns before node startup. Periodic Run, node, tunnel, and capability maintenance executes in independent, non-overlapping component loops. Allocation creation uses a bounded timeout per lifecycle item so cold image preparation cannot consume unrelated work budgets. On shutdown, active calls are canceled before the application waits for workers.

Runtime-slot admission consumes only axnoded's aggregate `runtime_slots` summary. Individual cgroup and interface pools are diagnostic details. `ReportNode` rejects summaries that omit `runtime_slots`; releases that add a required node-summary contract must rebuild controld and axnoded together.

Node reports also carry one atomic typed capability snapshot. Runsc memory and ephemeral-storage hard limits depend on matching conformance evidence; no alternate-runtime evidence can satisfy them. Controld derives workload requirements, rechecks current evidence while candidate rows are locked, and persists admitted dependencies with the allocation. Capability transitions use a queue separate from create/delete lifecycle work. The shared [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) document is the canonical contract for provider evidence and loss policy.

Rootfs locality covers local directories and registry images (OCI/Nydus), not raw object-store mounts.

Sandbox egress policy is normalized during API validation and contributes a derived DNS-policy or strict-egress capability requirement. A node without the matching current proof is ineligible; the policy is never forwarded as an optional field that an older runtime may ignore. See [Sandbox Network Policy](../../docs/architecture/sandbox-network-policy.md).

Node rows are durable identities with `active` and `retired` states. Placement and node authentication accept only active identities. `axern admin node retire` locks the node, requires a stale heartbeat, and rejects retirement while control-plane lifecycle work still references it. Successful retirement and its operator reason are committed with one audit event. `axern admin reliability check` evaluates only active nodes and reports stale heartbeat, stale summary, and non-ready axnoded counts.

## Build

```bash
make -C control/controld build
make -C control/controld build-migrate
make -C control/controld build-access-bootstrap
make -C control/controld build-retention
```

## Test

```bash
make -C control/controld test
make -C control/controld vet
make -C control/controld check-architecture
```

Postgres integration tests use the database named by `AXERN_TEST_POSTGRES_DSN`. The `test` target serializes Go packages whenever that variable is set because the integration fixtures reset one shared database between cases.

From the repository root:

```bash
make controld-test
make agent-doc-check
```

For cross-component runtime log meanings, see [Runtime Logs](../../docs/operations/runtime-logs.md).

## Run

```bash
bash ./scripts/dev-mtls-certs.sh
go run ./control/controld/cmd/controld \
  -grpc-address 127.0.0.1:24000 \
  -http-address 127.0.0.1:24001 \
  -heartbeat-freshness-window 15s \
  -summary-freshness-window 15s \
  -resource-cpu-overcommit-ratio 1.0 \
  -tls-ca-cert .dev/certs/ca.crt \
  -tls-cert .dev/certs/controld.crt \
  -tls-key .dev/certs/controld.key \
  -secrets-master-key "local-only-master-key-32-bytes!!" \
  -postgres-dsn "postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable"
```

Postgres schema migration and retention cleanup are separate entrypoints:

```bash
go run ./control/controld/cmd/migrate \
  -postgres-dsn "postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable" \
  up

go run ./control/controld/cmd/access-bootstrap \
  -postgres-dsn "postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable" \
  -certificate .dev/certs/client.crt

go run ./control/controld/cmd/retention \
  -postgres-dsn "postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable"
```

`controld` requires both migrations and access bootstrap to be complete. It refuses startup without an active platform administrator. Public product APIs accept only gatewayd-forwarded verified client fingerprints; direct public API calls to controld are rejected. See [Principal And Namespace Authorization](../../docs/architecture/authorization.md). `cmd/retention` assumes the database has already been initialized. Retention uses a Postgres advisory lock, so duplicate workers skip instead of racing. The cleanup policy covers tunnel session events, terminal runs, and expired or revoked execution leases; use the `-retention-*-ttl` and `-retention-*-keep` flags on `cmd/retention` for per-resource tuning.

Common optional environment variables:

- `CONTROLD_INSECURE_REGISTRIES`: comma-separated registry hosts that should be resolved over HTTP instead of HTTPS, used by local truth environments such as `localhost:5001` and `host.docker.internal:5001`.
- `-reconcile-timeout`: maximum duration of one background reconcile operation; `0` uses the application default of `30s`.

## API Surface

Admin product APIs:

- `sdk/proto/axern/control/admin/v1/audit.proto`
- `sdk/proto/axern/control/admin/v1/allocation_lifecycle.proto`
- `sdk/proto/axern/control/admin/v1/reliability.proto`
- `sdk/proto/axern/control/admin/v1/node.proto`

Public product APIs:

- `sdk/proto/axern/control/catalog/v1/catalog.proto`
- `sdk/proto/axern/control/environment/v1/environment.proto`
- `sdk/proto/axern/control/secret/v1/secret.proto`
- `sdk/proto/axern/control/gateway/v1/gateway.proto`
- `sdk/proto/axern/control/namespace/v1/namespace.proto`
- `sdk/proto/axern/control/run/v1/run.proto`
- `sdk/proto/axern/control/quota/v1/quota.proto`
- `sdk/proto/axern/control/tunnel/v1/tunnel.proto`

Control-plane coordination and internal calls:

- `sdk/proto/axern/control/node/v1/node_control.proto`
- `sdk/proto/axern/private/node/lifecycle/v1/lifecycle.proto`

Persistent-volume product APIs are not supported. Allocation-local writable files remain on the execution path and must be exported before Allocation cleanup.

The HTTP listener exposes diagnostics and internal runtime artifact downloads. Diagnostic endpoints are read-only:

- `/healthz`
- `/nodesz`
- `/resourcez`
- `/quotasz`
- `/catalogz`
- `/reconcilez` for background reconciler health
- `/allocation-reconcilez` for allocation lifecycle retry queue state
- `/consistencyz` for read-only reservation, lease, tunnel, and allocation consistency diagnostics

## Design Docs

- [Environment and catalog](docs/environment-and-catalog.md)
- [Node placement and leases](docs/node-placement-and-leases.md)
- [Reconcile operations](docs/reconcile-operations.md)
- [Observed capability providers](../../docs/architecture/observed-capability-providers.md)
- [Reconcile operations](docs/reconcile-operations.md)
- [Consistency repair boundaries](docs/consistency-repair-boundaries.md)
- [Resource admission](docs/resource-admission.md)
- [Resource quota](docs/resource-quota.md)
- [Postgres schema design](docs/postgres-schema-design.md)

## Architecture

```mermaid
flowchart LR
  app["internal/app\ncomposition root"]
  api["internal/api/*\ngRPC + debug HTTP adapters"]
  application["internal/application/*\nuse-case orchestration"]
  kernel["internal/kernel/*\ndomain contracts + rules"]
  postgres["internal/postgres/*\nPostgres durable adapters"]
  placement["internal/placement\ncandidate selection"]
  nodebridge["internal/nodebridge\nnode lifecycle bridge"]
  observability["internal/observability\nmetrics + spans"]
  ociimage["internal/ociimage\nOCI resolution"]
  catalog["internal/catalog\nruntime templates"]
  node["axnoded / node APIs"]
  db[("Postgres")]

  app --> api
  app --> application
  app --> postgres
  app --> placement
  app --> nodebridge
  app --> observability
  app --> ociimage
  app --> catalog

  api --> application
  application --> kernel
  application --> placement
  application --> nodebridge
  application --> ociimage
  application --> catalog

  postgres --> kernel
  postgres --> db
  nodebridge --> node
```

## Code Layout

- `cmd/controld`, `cmd/migrate`, and `cmd/retention` are executable entrypoints.
- `internal/app` is the composition root and lifecycle wiring layer.
- `internal/api/{adminv1,publicv1,nodev1,gatewayv1,debughttp}` adapts gRPC/HTTP to narrow capabilities.
- `internal/application/{admin,capability,environment,gateway,node,run}` owns use-case orchestration across kernel contracts and adapters, including node availability, capability-loss reconciliation, and workload lifecycle convergence.
- `internal/kernel/*` owns domain contracts, state transitions, and reusable control-plane rules. `internal/kernel/placement` carries request-scoped candidate plans and the pure preference ordering shared with durable admission.
- `internal/postgres/*` owns SQL-backed stores, row scanners, transaction helpers, migrations, Postgres-specific persistence details, and transactional reservation admission.
- `internal/placement` owns candidate filtering, eligibility evaluation, candidate-plan construction, and placement request shaping.
- `internal/nodebridge` owns control-plane-to-node lifecycle request construction and RPC bridging.
- `internal/observability`, `internal/ociimage`, and `internal/catalog` own metrics/span names, OCI descriptor resolution, and embedded runtime templates.
- `internal/testutil/controldtest` owns focused test doubles and Postgres test harness helpers.

Before changing package boundaries or feature placement rules, read [Agent Contract](AGENTS.md). `make -C control/controld check-architecture` enforces the main direction rules: API/application/kernel packages must not import Postgres adapters, Postgres adapters must not reintroduce alias bridges, and catch-all helper files should not return under `internal/postgres`.
