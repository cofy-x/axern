# controld Postgres Schema Design

Postgres is the authoritative store for `controld`. The canonical schema is defined by `internal/postgres/migrations/*.sql`; application startup validates the applied versions but never mutates the schema.

## Migration Layout

The rebuild-only schema has one canonical baseline:

| Migration | Ownership |
| --- | --- |
| `000001_initial.sql` | Principals, nodes, namespaces, environments, secrets, Runs, Allocations, reservations, execution leases, tunnels, reconciliation, and audit state |

Each migration declares the final shape of its domain. Migrations run in one direction under a Postgres advisory lock and are recorded in `schema_migrations` with version, name, checksum, and application time. The repository uses rebuild-only database upgrades: schema changes are folded into the owning baseline migration and the database is recreated. Compatibility migrations and dual-read paths are outside the current contract.

```mermaid
sequenceDiagram
  participant Migrate as controld-migrate
  participant DB as Postgres
  participant Control as controld
  participant Retention as retention worker

  Migrate->>DB: acquire advisory lock
  Migrate->>DB: validate applied versions and checksums
  Migrate->>DB: apply pending migrations transactionally
  Migrate->>DB: record version and checksum
  Control->>DB: require the complete schema
  Retention->>DB: require the complete schema
```

An edited checksum, a missing version, or a database ahead of the binary is a startup error. Rebuild the database whenever a baseline checksum changes.

## Core Control-Plane Model

```mermaid
erDiagram
  namespaces ||--o| namespace_resource_quotas : owns
  namespaces ||--o{ environments : scopes
  namespaces ||--o{ secrets : scopes
  environments ||--o{ runs : configures
  runs ||--|| allocations : executes
  nodes ||--o{ allocations : hosts
  nodes ||--|| node_summaries : reports
  allocations ||--o| reservations : reserves
  allocations ||--o{ execution_leases : authorizes
  allocations ||--o| allocation_reconcile_queue : retries
  allocations ||--o{ allocation_capability_requirements : requires
  allocations ||--o| allocation_capability_conditions : projects
```

### Catalog and namespace state

- `namespaces` is the durable scope and optimistic-lock row.
- `namespace_resource_quotas` stores optional CPU, memory, and ephemeral-storage admission limits.
- `environment_templates` stores versioned catalog entries.
- `environments.spec` stores user intent; `resolved_template` stores the normalized runtime snapshot used by execution paths.
- `namespace_quota_events` records durable admission decisions independently from operator audit events.

Namespace names are stored on scoped resources for filtering and ownership. Only relationships whose deletion semantics are part of the domain contract use database foreign keys.

### Secrets

`secrets` stores encrypted payloads and query-safe metadata:

- `data_keys` lists available keys without exposing values.
- `encrypted_payload` is never returned after creation. Execution configuration stores secret references, not plaintext.

### Runs and allocations

`runs` models one user-visible execution lifecycle and is the only durable owner of immutable execution config, public status, result, diagnostics, message, optimistic version, and user-visible timestamps. Every Run owns exactly one Allocation through the unique, non-null `allocations.run_id` foreign key. The uniqueness constraint is intentional: the product does not reschedule one Run onto multiple or replacement Allocations. A retry of execution is a new Run.

`allocations` is the concrete execution unit and infrastructure-convergence record. Its globally unique, never-reused `allocation_id` is the data-plane and cleanup identity; `run_id` is its sole owner and `node_id` is its immutable execution binding. `lifecycle_state` contains only `BOUND`, `STARTING`, `ACTIVE`, `RELEASING`, or `RELEASED`. A node `STOPPED` observation atomically commits result fields to the Run and persists the Allocation as `RELEASING`; it is never stored as an Allocation state. Allocation has no config, public status, exit result, diagnostic, message, version, or TaskSet-specific preparation columns.

`reservations` records admitted CPU, sandbox-memory, and ephemeral-storage requests. `sandbox_memory_request_bytes` is the public request without a runtime overhead side channel. A non-null `released_at` closes the control-plane reservation without erasing accounting history; node admission still honors a larger axnoded local commitment until host cleanup completes.

Allocation memory usage is live node-local diagnostic data rebuilt from the authoritative cgroup. It is not persisted by controld and does not participate in reservation or lifecycle decisions. The memory budget used during placement is validated inside the reservation transaction.

`allocation_capability_requirements` stores only the immutable typed key and catalog loss policy. It is owned through the Allocation foreign key and does not repeat Node binding or preserve placement observations. The Allocation binding, requirement rows, reservation, and create intent commit in one transaction; that transaction is the admission decision.

`allocation_capability_conditions` stores one complete latest diagnostic projection and its `observed_at`. Older reports are ignored, exact equal-time replay is idempotent, and conflicting equal-time data is rejected. Terminal Allocations ignore late reports. Conditions cannot update Run or Allocation lifecycle, readiness, exit information, or the primary message.

### Reconciliation and audit

`allocation_reconcile_queue` is the sole durable dispatcher for node Create/Delete operations. `next_run_at`, `reconcile_attempts`, and `last_error` describe delivery state; `lease_owner` and `lease_expires_at` provide renewable multi-worker claims. Run admission, cancellation, terminal observation, and create exhaustion write or replace this intent in the same transaction as their authoritative lifecycle changes. Completion and rescheduling require the current claim owner, so an expired worker cannot acknowledge newer work.

Capability loss has no controld queue or transition-history table. Axnoded owns the crash-safe Allocation-scoped verification/termination intent. Controld persists only the latest ordered Node summary and condition projection; its lifecycle queue remains limited to create/delete convergence.

`admin_audit_events` records operator mutations before lifecycle coordination state changes. It is distinct from quota decisions and workload event history.

## Nodes and Execution Leases

`nodes` stores identity, control target, authentication hash, heartbeat freshness, lifecycle status, retirement reason, and version. Active identities may report and participate in placement. Retirement is irreversible, retains historical references, and commits with an admin audit event after lifecycle and storage blockers are clear. `node_summaries` stores rich reported capacity and inventory. Runtime eligibility is not stored because Axern has one production execution boundary.

```mermaid
sequenceDiagram
  participant Gateway as "gatewayd"
  participant Control as "controld"
  participant DB as "Postgres"
  participant Node as "axnoded"

  Gateway->>Control: acquire execution lease
  Control->>DB: persist token hash, allocation and node identity, expiry, revision
  Control-->>Gateway: return plaintext token once
  DB-->>Node: notify lease stream changed
  Node->>Control: watch from last revision
  Control-->>Node: hashes, revocations, and expiries
```

`execution_leases` binds authorization to an exact Allocation ID and Node ID. Only token hashes are stored. `control_revisions` owns the monotonic revision stream, and the execution-lease trigger wakes watchers without making notifications authoritative state.

## Tunnel Model

```mermaid
erDiagram
  allocations ||--o{ tunnel_sessions : opens
  nodes ||--o{ tunnel_sessions : serves
  tunnel_sessions ||--o{ tunnel_session_events : records
```

`tunnel_sessions` stores the selected Allocation ID, remote port, edge and relay targets, encrypted node token, token hashes, revision, traffic counters, expiry, and revocation state. A partial unique index prevents two active sessions from claiming the same allocation port.

`tunnel_session_events` is append-only peer and lifecycle history. The `tunnel_sessions` control revision supports incremental node convergence.

## Query and Index Intent

Indexes follow server-side access paths:

- namespace and creation cursors for list APIs;
- node/lifecycle and namespace/run-status indexes for placement and lifecycle projection;
- partial active indexes for reservations, leases, tunnels, and live Allocations;
- retention indexes on expiry and creation timestamps;

New indexes require a concrete query, reconciliation, retention, or uniqueness contract. Low-cardinality status values are not indexed alone.

## Storage Rules

Typed columns own identity, state-machine status, foreign keys, optimistic versions, timestamps, budgets, usage totals, and fields used for ordering or selection. JSONB owns versioned intent and snapshots that are read and written as a whole.

Retention may delete completed history only after checking domain references. It must not delete current workloads or active leases.
