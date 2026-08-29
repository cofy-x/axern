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
- Keep host enforcement in `internal/enforcement`; nftables owns only the
  `inet axern_egress` table and must not mutate bridge/bpfnet SNAT or DNAT.
- DNS upstreams must come from the prepared axnoded proof. Never consult the
  host resolver or add a public fallback in egressd.
- Domain traffic requires both an unexpired DNS-derived IP authorization and a
  matching bounded HTTP Host or TLS SNI inspection before relay.

## Verification

Run `make egressd-test`, `make egressd-vet`, `make proto-generated-check`, and
the root Go checks after changing this module.
