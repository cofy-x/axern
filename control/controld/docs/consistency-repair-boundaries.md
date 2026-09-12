# Consistency Repair Boundaries

This document defines the long-term boundary between consistency diagnostics and repair writes. The checker is a read model. It finds mismatches across Runs, Allocations, reservations, leases, tunnel sessions, and lifecycle retry queue state, but it does not own mutation.

## Principle

Consistency repair must go through the owner that owns the state transition:

- Run controllers repair Run-owned Allocations, reservations, and leases;
- tunnel controllers repair tunnel session lifecycle state;
- audited admin operations repair durable lifecycle retry queue state when an operator chooses an explicit action.

The checker can classify and report. It must not silently release reservations, revoke leases, delete tunnel sessions, or edit Run/Allocation references.

```mermaid
flowchart LR
  Checker["consistency checker (read-only)"] --> Issues["typed issue codes"]
  Issues --> Operator["operator / CLI / smoke"]
  Operator --> Run["run owner repair"]
  Operator --> Tunnel["tunnel owner repair"]
  Operator --> Admin["audited admin repair"]
  Run --> DB[("Postgres")]
  Tunnel --> DB
  Admin --> DB
```

## Issue Ownership

The admin consistency API attaches a repair plan to every issue as typed `repair_owner`, `repair_action`, `repair_target_type`, `repair_target_id`, and `automatic_repair` fields. The first version intentionally reports `automatic_repair=false` for every issue.

| Issue family                           | `repair_owner`          | `repair_action`                | target                                                        |
| -------------------------------------- | ----------------------- | ------------------------------ | ------------------------------------------------------------- |
| active reservation on ended allocation | `run_controller`        | `run_cleanup`                  | `run/<run_id>` or `allocation/<allocation_id>`                |
| active lease on ended allocation       | `node_lifecycle`        | `node_lifecycle_reconcile`     | `allocation/<allocation_id>`                                  |
| active tunnel on ended allocation      | `tunnel_controller`     | `tunnel_lifecycle_reconcile`   | `tunnel_session/<session_id>` or `allocation/<allocation_id>` |

Foreign keys make missing-Allocation and allocation/node mismatch states unrepresentable. The checker therefore reports only lifecycle drift that relational constraints cannot prevent: an active reservation, lease, or tunnel on an ended Allocation. Those issues remain owner-reconciled because a generic checker cannot infer the correct transition without duplicating controller logic.

## Future Auto Repair

Auto repair can be added only as owner-scoped commands, not as checker writes. For example, a future `RepairRunAllocation` operation may lock the Run, validate its Allocation, reservation, and lease state, record an audit event, and then repair those references in one transaction.

An auto repair operation must satisfy all of these rules:

- lock the durable owner row before writing dependent state;
- verify the owner, allocation attempt, node, and terminal state inside the same transaction;
- emit an admin audit event when triggered by an operator;
- be idempotent when the reconciler already completed the cleanup;
- leave a typed event or diagnostic when it refuses to repair.

The checker remains the shared read model for detecting drift before and after the owner-specific repair runs.

## Owner-Scoped Repair API Model

The repair write surface should be owner scoped. Axern should not expose a generic `RepairConsistencyIssue` RPC because issue codes are diagnostics, not state-machine commands. Each future repair RPC must name the durable owner or dependent lifecycle object it will lock.

```mermaid
sequenceDiagram
  participant Operator
  participant AdminAPI as Admin repair API
  participant Owner as Owner controller/store
  participant DB as Postgres

  Operator->>AdminAPI: RepairRunAllocation(run_id, reason)
  AdminAPI->>Owner: validate repair command
  Owner->>DB: lock Run row
  Owner->>DB: load allocations, reservations, leases
  Owner->>Owner: apply owner state-machine rules
  Owner->>DB: write repair and audit atomically
  AdminAPI-->>Operator: repaired/refused with typed diagnostics
```

The intended command families are:

- `RepairRunAllocation`: lock the run row and repair one run-owned allocation, reservation, and lease set.
- `RepairTunnelSession`: lock the tunnel session and repair tunnel lifecycle state for the selected allocation.
- allocation lifecycle retry admin commands remain under the audited admin lifecycle API because the retry queue itself is the owner.

Every repair command must accept an operator reason, write an admin audit event, and return whether it repaired, refused, or found the state already converged. Refusals must use typed diagnostics so clients can show the next owner to inspect without parsing free-form text.
