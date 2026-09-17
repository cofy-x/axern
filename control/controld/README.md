# controld

`controld` is Axern's durable control-plane authority for Environment, Namespace, Run, gateway, tunnel, and Secret APIs. External CLI and SDK traffic should enter through `gatewayd`'s control edge; `controld` stays on the private control-plane network. It is backed by Postgres, which is the authoritative state store for control plane and node state.

`controld` owns:

- authenticated atomic node observations, heartbeat freshness, active inventory ingest, node-availability reconciliation, and audited irreversible node retirement
- authenticated allocation lifecycle batch ingest with durable Run projection
- Environment and Run lifecycle control
- Allocation resource admission, execution liveness authority, allocation access grants, and TunnelSessions
- namespace lifecycle, resource quota policy, quota admission, and quota usage reporting
- allocation terminal and tunnel relay target resolution
- controld-managed secret metadata, encryption, and resolution
- read-only operational debug HTTP surfaces

`controld` does not own realtime exec or terminal streaming. Realtime execution goes to selected nodes through the current SDK path, and `gatewayd` owns external control/data-plane forwarding after resolving routes here. Node lifecycle dispatch uses the dedicated `controld` workload certificate and verifies the exact bound Node URI; axnoded accepts that identity only for `NodeLifecycle`.

Run creation freezes the Environment source and resolved runtime input, then persists the Run, its single resource-charged Allocation, required Secret references, capability requirements, and node-create intent in one transaction. It returns before node startup. The reusable Environment row may later be physically deleted without changing execution or recovery for admitted Runs. Periodic Run, node, tunnel, and capability maintenance executes in independent, non-overlapping component loops. Allocation creation uses a bounded timeout per lifecycle item so cold image preparation cannot consume unrelated work budgets. On shutdown, active calls are canceled before the application waits for workers.

Runtime-slot admission consumes only axnoded's aggregate `runtime_slots` summary. Individual cgroup and interface pools are diagnostic details. `ReportNode` rejects summaries that omit `runtime_slots`; releases that add a required node-summary contract must rebuild controld and axnoded together.

Each `ReportNode` publication is one ordered atomic observation covering scheduling quantities, component health, capability evidence, memory-boundary validation, and separate diagnostics. Runsc memory and ephemeral-storage hard limits depend on matching conformance evidence; no alternate-runtime evidence can satisfy them. Controld derives immutable workload requirements, rechecks the current Node observation while candidate rows are locked, and persists those requirements with the Allocation. Capability changes are emitted only as metrics; there is no transition table or capability queue. The shared [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) document is the canonical contract for provider evidence and loss policy.

Rootfs locality covers local directories and registry images (OCI/Nydus), not raw object-store mounts.

Sandbox egress policy is normalized during API validation and contributes a derived DNS-policy or strict-egress capability requirement. A node without a matching current observation is ineligible; the policy is never forwarded as an optional field that a runtime may ignore. See [Sandbox Network Policy](../../docs/architecture/sandbox-network-policy.md).

Node rows are durable identities with `active`, `revoked`, and `retired` states. Placement and node authentication accept only active identities. `axern admin node retire` locks the node, requires a stale heartbeat, and rejects retirement while control-plane lifecycle work still references it. Successful retirement and its operator reason are committed with one audit event. `axern admin reliability check` evaluates only active nodes and reports stale heartbeat, stale summary, and non-ready axnoded counts.

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
make controld-postgres-test
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
  -workload-cluster axern.local \
  -workload-bundle .dev/certs/controld.pem \
  -workload-signer-bundle .dev/certs/private/signer.pem \
  -enrollment-address 127.0.0.1:24002 \
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

`controld` requires both migrations and access bootstrap to be complete. It refuses startup without an active platform administrator. Public product APIs accept only gatewayd-forwarded verified client fingerprints; direct public API calls to controld are rejected. See [Principal And Namespace Authorization](../../docs/architecture/authorization.md). `cmd/retention` assumes the database has already been initialized. Retention uses a Postgres advisory lock, so duplicate workers skip instead of racing. The cleanup policy covers tunnel session events, terminal runs, and expired or revoked allocation access grants; use the `-retention-*-ttl` and `-retention-*-keep` flags on `cmd/retention` for per-resource tuning.

Common optional environment variables:

- `CONTROLD_INSECURE_REGISTRIES`: comma-separated registry hosts that should be resolved over HTTP instead of HTTPS, used by local truth environments such as `localhost:5001` and `host.docker.internal:5001`.
- `-reconcile-timeout`: maximum duration of one background reconcile operation; `0` uses the application default of `30s`.

## API Surface

Admin product APIs:

- `sdk/proto/axern/control/admin/v1/audit.proto`
- `sdk/proto/axern/private/control/admin/v1/allocation_lifecycle.proto` (operator-only retry repair)
- `sdk/proto/axern/control/admin/v1/reliability.proto`
- `sdk/proto/axern/control/admin/v1/node.proto`

Public product APIs:

- `sdk/proto/axern/control/environment/v1/environment.proto`
- `sdk/proto/axern/control/secret/v1/secret.proto`
- `sdk/proto/axern/private/control/gateway/v1/gateway.proto`
- `sdk/proto/axern/control/namespace/v1/namespace.proto`
- `sdk/proto/axern/control/run/v1/run.proto`
- `sdk/proto/axern/control/quota/v1/quota.proto`
- `sdk/proto/axern/control/tunnel/v1/tunnel.proto`

Control-plane coordination and internal calls:

- `sdk/proto/axern/private/control/node/v1/node_control.proto`
- `sdk/proto/axern/private/node/lifecycle/v1/lifecycle.proto`

Persistent-volume product APIs are not supported. Allocation-local writable files remain on the execution path and must be exported before Allocation cleanup.

The HTTP listener exposes diagnostics and internal runtime artifact downloads. Diagnostic endpoints are read-only:

- `/healthz`
- `/nodesz`
- `/resourcez`
- `/quotasz`
- `/reconcilez` for background reconciler health
- `/allocation-reconcilez` for allocation lifecycle retry queue state
- `/consistencyz` for read-only Allocation resource-charge, access-grant, TunnelSession, and lifecycle consistency diagnostics

## Design Docs

- [Environment resolution](docs/environment-resolution.md)
- [Node placement and leases](docs/node-placement-and-leases.md)
- [Reconcile operations](docs/reconcile-operations.md)
- [Observed capability providers](../../docs/architecture/observed-capability-providers.md)
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
  templates["internal/environmenttemplate\ndeployment template inputs"]
  node["axnoded / node APIs"]
  db[("Postgres")]

  app --> api
  app --> application
  app --> postgres
  app --> placement
  app --> nodebridge
  app --> observability
  app --> ociimage
  app --> templates

  api --> application
  application --> kernel
  application --> placement
  application --> nodebridge
  application --> ociimage
  application --> templates

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
- `internal/postgres/*` owns SQL-backed stores, row scanners, transaction helpers, migrations, Postgres-specific persistence details, and transactional resource admission.
- `internal/placement` owns candidate filtering, eligibility evaluation, candidate-plan construction, and placement request shaping.
- `internal/nodebridge` owns control-plane-to-node lifecycle request construction and RPC bridging.
- `internal/observability`, `internal/ociimage`, and `internal/environmenttemplate` own metrics/span names, OCI descriptor resolution, and embedded environment templates.
- `internal/testutil/controldtest` owns focused test doubles and Postgres test harness helpers.

Before changing package boundaries or feature placement rules, read [Agent Contract](AGENTS.md). `make -C control/controld check-architecture` enforces the main direction rules: API/application/kernel packages must not import Postgres adapters, Postgres adapters must not reintroduce alias bridges, and catch-all helper files should not return under `internal/postgres`.

## Allocation Output And Access Grants

The first Allocation cleanup transition freezes a 15-minute output expiry in PostgreSQL. EnsureAbsent delivers that same deadline to axnoded; release does not extend it. Terminal Run retention waits for output expiry. Gateway resolution issues an exact-purpose grant under the Allocation row lock: interactive grants require ACTIVE, while output-only grants can read an unexpired cleanup snapshot. Grant issue/revoke and a per-Node delivery cursor commit together. Revocation publishes all of an Allocation's tokens in one update; other Nodes do not contend on a global grant cursor. The node ignores stale revisions and cannot use output grants for interactive operations.

## Node Identity Ownership And Recovery

| Fact | Owner |
| --- | --- |
| Node admission, revocation, retirement and initial token hash | PostgreSQL Node row; audited admin transaction |
| One CSR registration and retry response | Enrollment receipt, committed under the Node lock |
| Node private key and accepted certificate | Node root `identity/node.pem`; mode 0600 atomic bundle |
| Pending registration key and CSR | Node `identity/node.pem.pending`, removed after durable publication |
| Signing authority | Deployment private signer bundle, mounted only by controld |
| Allocation execution lifetime | ExecutionLease, independent of certificate lifetime |

The dedicated TLS enrollment listener exposes only EnrollNode and RenewNodeCertificate. Initial enrollment authenticates the server URI before transmitting the token. The transaction locks the admitted Node, checks database time against admission plus one hour, validates the token hash, and signs only the first CSR. Exact retries return the committed certificate; another CSR is rejected. Signing failures roll back. Retention removes expired receipts, but the immutable admission deadline prevents replay after removal.

Renewal requires the exact verified Node URI and an active Node row locked against revocation and retirement. Existing TLS connections are checked again at RPC time, including certificate-chain expiry. Normal NodeControl requests contain no enrollment token and must match the authenticated Node ID. Watches periodically recheck admission and terminate on expiry. `axern admin node revoke` withdraws all Node authority in an audited transaction even when the host is busy. It does not assert cleanup. New placement, execution-authority snapshots, start dispatch, access grants and Tunnel admission reject revoked identities; Tunnel peers revalidate their binding. Existing in-flight requests and issued authority remain bounded by their normal TTLs. The existing node-availability worker drives Allocation termination and retryable cleanup; no revocation queue or certificate shadow state is introduced. `retire` still requires an idle, disconnected Node with no remaining execution or access obligations. For a compromised host, external isolation/fencing is necessary: revocation cannot prove the kernel stopped.

Nodes register their own keys, retry registration using the persisted CSR after response loss, and renew 24-hour certificates with seven to eight hours remaining, using bounded jittered retry. Renewal never changes Node or Allocation identity. A corrupt, expired or mismatched existing bundle does not fall back to enrollment. The maintenance loop does not gate runtime recovery or the execution-lease watchdog.

### Deployment Rotation

Use `axern admin pki bootstrap --directory <private-directory> --cluster <domain>` to initialize deployment authority. Serialize operations on that directory and back it up. The tool never generates Node keys. Service certificates last 90 days; `--renew-services` rotates their leaves without changing the CA or administrator fingerprint. Republish the role Secret and restart service connections. Compose single-file bind mounts require container recreation after atomic host-file replacement; Kubernetes Secret volumes must not use subPath for reloadable credentials.

CA replacement is explicit: provision a new private directory, publish old-plus-new trust to every peer first, then publish the new signer and service bundles. Keep old trust for at least a full Node certificate lifetime after the last old issuance and confirm Node renewal before removing it. Reload failures fail closed. Do not rerun bootstrap over an overlapping trust bundle or silently replace a missing CA. Administrator credentials belong to AccessAdmin and must be rotated explicitly before their expiry; service renewal never changes their fingerprint.

Node bootstrap tokens are read-only file inputs, not node TOML or container environment contents. Existing published identities never read the token again; deployments may remove their bootstrap Secret after registration. Explicit Node IDs cannot be inferred from hostnames, and a new host or lost identity requires a new Node ID.
