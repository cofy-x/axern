# egressd Agent Contract

`runtime/egressd` is the trusted node-local owner of sandbox egress policy
state and enforcement lifecycle.

## Boundaries

- Keep `cmd/egressd` as a thin process entrypoint.
- Keep transport validation and status mapping in `internal/api`.
- Keep normalization, allocation-attempt fencing, persistence, recovery, and
  reconciliation in `internal/policy`.
- Reuse `lib/go/networkpolicy`; do not create a second public policy grammar.
- Never put the daemon, its socket, routing bypass marks, or `NET_ADMIN` inside
  a workload namespace.
- Persist state before reporting a lifecycle mutation as successful.
- Delete and reconcile must be fenced by the exact allocation attempt; stale
  work may never remove a newer policy.

## Verification

Run `make egressd-test`, `make egressd-vet`, `make proto-generated-check`, and
the root Go checks after changing this module.
