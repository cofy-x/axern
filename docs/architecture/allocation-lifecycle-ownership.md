# Allocation lifecycle and resource ownership

This document records the implemented lifecycle contract. It is intentionally about durable facts and failure boundaries, not controller implementation names.

## Facts found in the previous implementation

- Run admission committed a `Run`, a strict one-to-one `Allocation`, a strict one-to-one `Reservation`, capability requirements, and an Allocation create queue item in one PostgreSQL transaction.
- `Reservation` had no identity, owner, or transition independent of its Allocation. Its resource quantities duplicated the immutable Run execution request; `released_at` duplicated `Allocation.RELEASED`. Every accounting query joined it back through Allocation to Run.
- The object named `ExecutionLease` was created only when gatewayd resolved an interactive or output request. It authorized that one data-plane call by a bearer-token hash. It was never consulted by runsc, lifecycle create/delete, cgroup, egress, SSH, or Tunnel cleanup and therefore could not fence execution during a control-plane partition.
- Allocation create/delete intent was durable in `allocation_reconcile_queue`. Worker claim owner and expiry were delivery fencing, not Allocation identity. Request handlers changed the authoritative Run/Allocation state and the queue intent in the same transaction.
- axnoded's `AllocationState` was the durable local admission contract. Runtime inventory, cgroup state, egress state, and container metadata were recovered as projections. A narrow terminal outbox retained exact runtime exit evidence until controld acknowledged it.
- A normal axnoded shutdown explicitly deleted all Allocations. This made process restart behave like workload cancellation and defeated the recovery model, even though restart code could recover live runsc sandboxes.
- Node inventory absence and stale-node handling synthesized a STOPPED observation in controld. Inventory absence is useful failure evidence but is not a node-authored exit result; a late STARTING/RUNNING observation was already rejected after the resulting terminal transition.

## Authoritative model

| Fact | Owner | Durable representation |
| --- | --- | --- |
| User request, public status, final result | Run | PostgreSQL `runs` |
| Concrete identity, node binding, infrastructure state, resource charge | Allocation | PostgreSQL `allocations` |
| Create/delete delivery intent | controld lifecycle dispatcher | PostgreSQL `allocation_reconcile_queue` row |
| Permission for an Allocation to keep executing | controld | Complete authorized-Allocation snapshot returned by authenticated node heartbeat |
| Gateway data-plane authority | controld | PostgreSQL `allocation_access_grants` token hash and delivery revision |
| Immutable node admission contract | axnoded | Node `AllocationState` |
| Actual live/exited runtime state | runsc | Runtime inventory plus minimal exit checkpoint |
| Undelivered terminal result | axnoded | Terminal lifecycle outbox |
| Tunnel lifetime | TunnelSession | PostgreSQL `tunnel_sessions` |

Resource reservation is not a second object. The Allocation row is the transactional resource charge from admission until `RELEASED`. Queue claims and retry counters are delivery metadata. Capability conditions and inventory are observations and cannot advance desired state.

## State machine

`BOUND -> STARTING -> ACTIVE -> RELEASING -> RELEASED`

- controld admission creates `BOUND` and the create intent atomically.
- A node STARTING or ACTIVE observation may advance a nonterminal Allocation.
- A node STOPPED observation supplies the candidate Run result; controld atomically makes the Run terminal, changes the Allocation to `RELEASING`, revokes subordinate access, and replaces create intent with delete intent.
- User cancellation and control-plane failure decisions may also move directly to `RELEASING`, but cannot invent a node exit code.
- Only an acknowledged idempotent node delete moves `RELEASING` to `RELEASED`.
- Terminal Run states and Allocation cleanup states never move backwards.

## Crash and ordering guarantees

- PostgreSQL transactions couple admission, binding, resource charge, and create intent; cancellation/result and delete intent are coupled likewise.
- Dispatcher claims are renewable and fenced. A stale worker cannot complete or reschedule an operation after another worker takes ownership.
- Node create/delete is idempotent on Allocation ID and the immutable create digest. Allocation ID is never reused.
- Exact terminal runtime evidence is persisted before runtime artifacts may be removed and is retried until acknowledged.
- Duplicate, delayed, and wrong-node observations cannot change a terminal Run or revive an Allocation.
- Process shutdown stops node observers and preserves admitted live sandboxes; it does not translate service restart into Allocation deletion.
- Each successful authenticated heartbeat replaces the node's entire execution authority set. TTL starts from the node receipt clock; omission revokes immediately, and expiry persists across restart. If authority cannot be recovered or renewed, the node records an `EXECUTION_LEASE_EXPIRED` termination intent and stops the Allocation fail-closed.
- Runtime, cgroup, egress, SSH, Tunnel, execution leases, and access grants all use the same Allocation ID. Gateway access grants never authorize runtime liveness, and execution leases never authorize a user data-plane operation.
