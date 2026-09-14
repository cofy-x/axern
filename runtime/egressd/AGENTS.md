# Egress Policy Agent Contract

## Purpose

`runtime/egressd` is the trusted node-local owner of sandbox egress-policy state and enforcement lifecycle. Read the [egressd README](README.md) for runtime behavior and configuration.

## Ownership Boundaries

- Keep `cmd/egressd` thin, transport validation and status mapping in `internal/api`, policy normalization/persistence/recovery in `internal/policy`, and host enforcement in `internal/enforcement`.
- Reuse `lib/go/networkpolicy`; do not create another public policy grammar.
- Keep the daemon, socket, routing bypass marks, and `NET_ADMIN` outside workload namespaces.
- Persist the complete normalized policy record before reporting success. Its globally unique Allocation ID is the sole ownership fence; axnoded reconciles with the authoritative set of locally admitted Allocation IDs, never a second digest/revision proof copy.
- Nftables owns only the `inet axern_egress` table and must not mutate bridge or bpfnet egress-SNAT state.
- Use the prepared axnoded DNS proof; never consult the host resolver or add a public fallback.
- Domain traffic requires both an unexpired DNS-derived IP authorization and bounded per-request HTTP Host or TLS SNI validation.

## Validation

- Run `make egressd-test`, `make egressd-vet`, the relevant proto/generated checks, and Linux enforcement truth selected by `make verify-changed`.
