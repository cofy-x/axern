# Tunnel Data Plane Agent Contract

## Purpose

`runtime/tunneld` owns the raw TCP tunnel relay, node-side peer, and sandbox tunnel agent. Read the [Tunnel Runtime README](README.md) for binaries, deployment, and data flow.

## Ownership Boundaries

- `controld` owns tunnel lifecycle, authorization, leases, events, and status; tunneld owns only in-memory relay and peer state.
- Bind every session to an explicit Allocation and attempt-scoped authorization from the [Stable Domain Model](../../docs/product/domain-model.md).
- Keep relay validation, pairing, revalidation, forwarding, limits, and metrics in `internal/relay`; keep runsc/netns and node lifecycle concepts in the node-side command.
- Keep the tunnel agent small and static-build-friendly, and keep application protocols above this raw TCP module.
- Preserve restart tolerance while controld remains authoritative. Keep metrics low-cardinality and never expose session tokens.
- Keep standalone relay artifacts separate from node-side binaries and update deployment wiring when binary paths or flags change.

## Validation

- Run `go test ./...` and build `cmd/tunneld`, `cmd/node-tunneld`, and a static `cmd/tunnel-agent` from this module.
- Run the relevant Compose/SDK tunnel truth selected by `make verify-changed` for netns, runsc injection, TLS, status, pairing, or revalidation changes.
