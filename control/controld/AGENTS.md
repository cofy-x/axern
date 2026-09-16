# Control Plane Agent Contract

## Purpose

`control/controld` owns Axern's durable control-plane state and lifecycle. Use the [Control Plane README](README.md) for commands and API lookup; read the contracts selected by the task below.

## Task Routes

| Task | Required local context |
| --- | --- |
| Schema, persistence ownership, admission transaction, or lock ordering | [Postgres Schema Design](docs/postgres-schema-design.md) |
| Placement, lifecycle reports, lease, or retry queue | [Node Placement And Leases](docs/node-placement-and-leases.md) |
| Resource charge or quota | [Resource Admission](docs/resource-admission.md), [Resource Quota](docs/resource-quota.md) |
| Principal credentials or namespace access | [Authorization](../../docs/architecture/authorization.md) |
| Node registration, renewal, revocation, or retirement | [Node Identity Ownership And Recovery](README.md#node-identity-ownership-and-recovery) |
| Capability observation or placement policy | [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) |

## Ownership Boundaries

- PostgreSQL is the sole authority for Environment, Run, Allocation binding and resource charge, placement, AllocationAccessGrant, TunnelSession, and control-plane lifecycle state. ExecutionLease has no control-plane table; it is derived from current Allocation authority on every authenticated Node heartbeat.
- Keep public contracts in `sdk/proto`. Realtime terminal, execution, and Allocation-local output traffic belongs to the data plane, not controld HTTP.
- Principal Credentials have explicit protocol kinds, expiry, and revocation. SSH and X.509 share namespace RoleBindings, but only an active X.509 credential can satisfy the last platform administrator invariant. Bootstrap must atomically register initial credentials and must never reactivate revoked material on retry.
- Keep RPC validation and mapping in `internal/api`, use-case orchestration in `internal/application`, domain contracts and pure rules in `internal/kernel`, SQL and transaction mechanics in `internal/postgres`, and construction/lifecycle in `internal/app`.
- `internal/application` and `internal/kernel` must not depend on Postgres implementations. API and composition packages depend on narrow capabilities rather than concrete stores.
- Keep placement separate from node execution. Gateway resolution returns an explicit Allocation target and AllocationAccessGrant bound to that exact Allocation ID.
- Use the dedicated `controld` workload certificate for node lifecycle dispatch. Axnoded authorizes it only for `NodeLifecycle`; controld must never acquire `NodeSandbox` or node-operator authority.
- Run admission must freeze the normalized and resolved Environment specifications and protect every required Secret reference in the same transaction as the Run, its single Allocation, resource charge, capability requirements, and create intent. Recovery must not depend on the continued existence of the reusable Environment row.
- Lifecycle transactions lock the Allocation before its reconcile-queue row. Preserve this order in request, report, worker-completion, cancellation, and release paths.
- Node lifecycle uses a durable bounded queue; capability placement reads the current Node observation transactionally, while post-bind capability loss is owned by axnoded's Allocation-scoped fail-stop intent. Event paths must not trigger unbounded scans.
- Shared platform capability keys and provider/loss policy belong to `lib/go/nodecapability`; do not duplicate them in controld.
- Keep debug HTTP read-only. Durable retry operations and audit/reliability models belong to the admin application and Postgres boundaries.
- Node revocation withdraws authority without claiming cleanup. Keep it in the Node lifecycle and existing audit/Allocation reconciliation paths; never bypass retirement blockers or introduce a certificate lifecycle shadow.
- Node enrollment signs one CSR under the admitted Node row lock; normal NodeControl requires exact URI identity and current admission, never enrollment tokens. Keep signing material control-only.

## Validation

- Run `make -C control/controld test`, `make -C control/controld vet`, `make -C control/controld check-architecture`, and `test -z "$(gofmt -l control/controld)"`.
- For placement, node lifecycle, reporting, or capability changes, run the relevant axnoded and Linux truth checks selected by `make verify-changed`.
