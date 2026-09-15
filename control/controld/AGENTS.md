# Control Plane Agent Contract

## Purpose

`control/controld` owns Axern's durable control-plane state and lifecycle. Read the [Control Plane README](README.md) for API, package, and document routing.

## Ownership Boundaries

- PostgreSQL is the sole authority for Environment, Run, Allocation binding and resource charge, placement, AllocationAccessGrant, TunnelSession, and control-plane lifecycle state. ExecutionLease has no control-plane table; it is derived from current Allocation authority on every authenticated Node heartbeat.
- Keep public contracts in `sdk/proto`. The HTTP listener is limited to diagnostics; realtime terminal, execution, and Allocation-local output traffic belongs to the data plane.
- Keep RPC validation and mapping in `internal/api`, use-case orchestration in `internal/application`, domain contracts and pure rules in `internal/kernel`, SQL and transaction mechanics in `internal/postgres`, and construction/lifecycle in `internal/app`.
- `internal/application` and `internal/kernel` must not depend on Postgres implementations. API and composition packages depend on narrow capabilities rather than concrete stores.
- Keep placement separate from node execution. Gateway resolution returns an explicit Allocation target and AllocationAccessGrant bound to that exact Allocation ID.
- Use the dedicated `controld` workload certificate for node lifecycle dispatch. Axnoded authorizes it only for `NodeLifecycle`; controld must never acquire `NodeSandbox` or node-operator authority.
- Run admission must freeze the normalized and resolved Environment specifications and protect every required Secret reference in the same transaction as the Run, its single Allocation, resource charge, capability requirements, and create intent. Recovery must not depend on the continued existence of the reusable Environment row.
- Allocation lifecycle ingest authenticates and resolves owners in batches, locks deterministically, and persists each affected Allocation and Run once per batch.
- Lifecycle transactions lock the Allocation before its reconcile-queue row. Preserve this order in request, report, worker-completion, cancellation, and release paths.
- Node lifecycle uses a durable bounded queue; capability placement reads the current Node observation transactionally, while post-bind capability loss is owned by axnoded's Allocation-scoped fail-stop intent. Event paths must not trigger unbounded scans.
- Shared platform capability keys and provider/loss policy belong to `lib/go/nodecapability`; controld owns durable observation, placement admission, dependency conditions, and reconciliation.
- Keep debug HTTP read-only. Durable retry operations and audit/reliability models belong to the admin application and Postgres boundaries.
- The implementation follows the [Stable Domain Model](../../docs/product/domain-model.md); writable files and output bytes remain Allocation-local and Axern defines no generic Artifact root.

## Validation

- Run `make -C control/controld test`, `make -C control/controld vet`, `make -C control/controld check-architecture`, and `test -z "$(gofmt -l control/controld)"`.
- For proto changes, run the repository proto generation and generated-output checks before compilation.
- For placement, node lifecycle, reporting, or capability changes, run the relevant axnoded and Linux truth checks selected by `make verify-changed`.
