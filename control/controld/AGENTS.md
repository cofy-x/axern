# Control Plane Agent Contract

## Purpose

`control/controld` owns durable product state, admission, placement, authorization, and lifecycle convergence. Use the [Control Plane README](README.md) for commands.

## Task Routes

| Task | Required local context |
| --- | --- |
| Schema, transactions, or persistence ownership | [Postgres Schema Design](docs/postgres-schema-design.md) |
| Placement, lifecycle, leases, or delivery | [Node Placement And Leases](docs/node-placement-and-leases.md) |
| Resource charge or quota | [Resource Admission](docs/resource-admission.md), [Resource Quota](docs/resource-quota.md) |
| Principal credentials or namespace access | [Authorization](../../docs/architecture/authorization.md) |
| Node registration, renewal, revocation, or retirement | [Node Identity Ownership And Recovery](README.md#node-identity-ownership-and-recovery) |
| Capability observation or placement policy | [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) |

## Ownership Boundaries

- PostgreSQL is the sole authority for public resources, Allocation binding and charge, access grants, TunnelSessions, and control-plane lifecycle state.
- Admission atomically freezes Run input, creates its single Allocation and resource charge, protects required Secrets, and records lifecycle delivery intent.
- Keep public control state separate from node execution and realtime data-plane traffic. Gateway resolution returns authority bound to one exact Allocation ID.
- Keep domain and application code independent from Postgres implementations; transport, transaction, and composition details remain in their adapters.
- Node identity, credentials, revocation, retirement, and lifecycle dispatch must use explicit typed identities and least-privilege workload authority; retries cannot reactivate revoked authority or bypass cleanup blockers.
- Capability keys and policies come from `lib/go/nodecapability`; observations are transactional admission evidence, not a second desired-state owner.

## Validation

Run `make -C control/controld test`, `make -C control/controld vet`, and `make -C control/controld check-architecture`; use `make verify-changed` for cross-component or Linux truth checks.
