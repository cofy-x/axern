# Consistency Diagnostics And Mutation Ownership

The consistency checker is a read model over Runs, Allocations, AllocationAccessGrants, TunnelSessions, and lifecycle delivery intent. It reports typed issue codes, affected identities, observed status, and diagnostic detail.

Diagnostics do not carry an executable repair plan. The API and CLI do not claim that a repair exists or predict a future controller action.

## Mutation Ownership

Normal lifecycle convergence remains with the Run and TunnelSession owners. Explicit administrative lifecycle retry commands validate the owner, lock the relevant records, and write their audit event in the same transaction. Node-local break-glass operations follow the Allocation recovery and terminal reporting contract.

The checker never releases resource charges, revokes access grants, deletes TunnelSessions, or edits Run and Allocation facts. Its response can be used to inspect an owner and to check convergence after an authorized operation.

## Relational And Lifecycle Checks

Foreign keys prevent missing Allocation references and mismatched Allocation/Node bindings. The checker reports lifecycle inconsistencies that relational constraints alone cannot prevent. Resource charges are columns on Allocation and cannot drift into a separate lifecycle.

Each diagnostic must describe a current invariant and a real query. Adding a diagnostic does not add an administrative mutation API.
