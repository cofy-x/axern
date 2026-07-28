# AGENTS.md

## Purpose

This is the agent contract for `runtime/tunneld`. Keep changes focused on the
raw TCP tunnel data plane and aligned with `controld`, `axnoded`, and CLI
callers.

## Ownership

- [`cmd/tunneld`](cmd/tunneld): relay process flags, wiring, gRPC server, and lifecycle.
- [`cmd/node-tunneld`](cmd/node-tunneld): node-local session watcher, netns binding, runsc agent launch, and node peer frame pump.
- [`cmd/tunnel-agent`](cmd/tunnel-agent): small static-build-friendly peer helper injected into runsc sandboxes.
- [`internal/relay`](internal/relay): peer validation, pairing, revalidation, frame forwarding, session limits, and relay metrics.
- [`internal/control`](internal/control), [`internal/relaytls`](internal/relaytls), [`internal/netns`](internal/netns), and [`internal/observability`](internal/observability): narrow support packages.

## Rules

- `controld` owns tunnel lifecycle, auth, leases, events, and status semantics. `tunneld` owns only in-memory relay state.
- Keep application protocol adapters out of this module. Claude/OpenAI/HTTP behavior belongs in CLI or gateway adapters above the raw TCP tunnel.
- Keep `cmd/*` private command logic in the command package. Split large command files by responsibility before creating shared `internal` packages.
- Do not let `internal/relay` depend on node-local concepts such as runsc, netns paths, `NodeOperator`, or allocation mechanics beyond validated session IDs.
- Preserve restart tolerance: `tunneld` and `node-tunneld` may restart while `controld` remains the source of truth.
- Keep relay metrics low-cardinality. Do not label metrics with session IDs, allocation IDs, or tokens.
- Deployment ownership matters: `tunneld` ships in the standalone tunneld image; `node-tunneld` and `tunnel-agent` ship in the node runtime / `node-all-in-one` image.
- If command flags, binary paths, Make targets, Dockerfiles, or compose/kind wiring change, update the related docs and deployment scripts together.

## Validation

For local changes under `runtime/tunneld`:

```bash
go test ./...
go build ./cmd/tunneld
go build ./cmd/node-tunneld
CGO_ENABLED=0 go build ./cmd/tunnel-agent
```

For netns binding, runsc agent injection, relay TLS, session status reporting,
or relay pairing/revalidation changes:

```bash
make local-compose-up
make local-compose-tunnel-e2e
```

`make local-compose-up` rebuilds local images by default unless
`AXERN_SKIP_LOCAL_IMAGES_BUILD=1` is set.
