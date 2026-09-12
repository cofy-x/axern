# Control Plane Agent Contract

## Purpose

`control/controld` owns Axern's durable control-plane state and lifecycle. Read the [Control Plane README](README.md) for API, package, and document routing.

## Ownership Boundaries

- PostgreSQL is the sole authority for Environment, Run, Allocation, placement, reservation, lease, tunnel-session, and control-plane lifecycle state.
- Keep public contracts in `sdk/proto`. The HTTP listener is limited to diagnostics and internal artifact delivery; realtime terminal and execution traffic belongs to the data plane.
- Keep RPC validation and mapping in `internal/api`, use-case orchestration in `internal/application`, domain contracts and pure rules in `internal/kernel`, SQL and transaction mechanics in `internal/postgres`, and construction/lifecycle in `internal/app`.
- `internal/application` and `internal/kernel` must not depend on Postgres implementations. API and composition packages depend on narrow capabilities rather than concrete stores.
- Keep placement separate from node execution. Gateway resolution returns an explicit Allocation target and attempt-scoped lease.
- Allocation status ingest authenticates and resolves owners in batches, locks deterministically, and persists each affected Allocation and Run once per batch.
- Node lifecycle and capability reconciliation use durable bounded queues; event paths must not trigger unbounded scans, and dispatch must enforce global and per-node concurrency limits.
- Shared platform capability keys and provider/loss policy belong to `lib/go/nodecapability`; controld owns durable observation, placement admission, dependency conditions, and reconciliation.
- Keep debug HTTP read-only. Durable retry operations and audit/reliability models belong to the admin application and Postgres boundaries.
- The implementation follows the [Stable Domain Model](../../docs/product/domain-model.md); storage remains allocation-local except for explicit artifact delivery.

## Validation

- Run `make -C control/controld test`, `make -C control/controld vet`, `make -C control/controld check-architecture`, and `test -z "$(gofmt -l control/controld)"`.
- For proto changes, run the repository proto generation and generated-output checks before compilation.
- For placement, node lifecycle, reporting, or capability changes, run the relevant axnoded and Linux truth checks selected by `make verify-changed`.
