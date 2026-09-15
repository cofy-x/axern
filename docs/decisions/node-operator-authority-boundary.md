# Node Operator Authority Boundary

## Decision

Keep Allocation-scoped `Exec`, `ExecStream`, and `Wait` as privileged node-local diagnostic capabilities. They operate only inside an Allocation with an authoritative node recovery record and exact runtime identity, reuse the normal process/runtime implementation, and do not become a second source of Allocation lifecycle truth. A control-plane-bound Allocation additionally requires a live execution lease for exec; explicit local conformance Allocations remain unbound and cannot acquire tunnel authority.

Do not expose ordinary node-local `Kill` or `Delete` alternatives to the control-plane lifecycle. Any destructive incident-recovery operation must be explicitly named as break-glass, restricted to an operator principal, observable, and coupled to the same terminal reporting and idempotent cleanup obligations as normal lifecycle handling.

Separate human operator authority from node-local machine APIs. A component needing only a narrow operation such as Allocation network resolution must not gain ambient access to exec, termination, cleanup, or unrestricted diagnostics. Operator endpoints must never be world-writable.

## Rationale

Removing local exec would make node and sandbox failures unnecessarily difficult to diagnose and would duplicate lower-level tools outside Axern. Allowing the same interface to create or destroy Allocation lifecycle facts would instead introduce a competing authority that can diverge from controld, lose terminal evidence, or invalidate recovery. Sharing that authority with node-local daemons also turns a narrowly trusted component into a path to arbitrary sandbox execution.

## Consequences

- The globally unique Allocation ID is the only target identity for local operator actions.
- Exec and wait handlers validate the authoritative node record and matching runtime and fail closed on ambiguity.
- Break-glass termination or cleanup cannot erase node recovery state before terminal evidence and cleanup obligations are satisfied.
- Human operator and machine sockets, services, deployment identities, and permissions follow least privilege.
- SDK and gateway authorization remain separate from the local operator principal; no local convenience API becomes a public product lifecycle.

## Revisit condition

Remove the local exec surface only if another Allocation-scoped path provides equivalent diagnosis during gateway or control-plane incidents. Combine operator and machine endpoints only if the transport enforces authenticated per-method authorization that preserves the same least-privilege boundary.
