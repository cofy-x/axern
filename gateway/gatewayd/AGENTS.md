# gatewayd Agent Contract

## Purpose

`gatewayd` is Axern's external control and data-plane gateway. It does not own
placement, lifecycle, or durable state. It proxies public control APIs to
`controld`, resolves service routes and terminal leases through `controld`, then
forwards data-plane traffic to `axnoded` nodes.

## Layout

- `main.go`: process bootstrap only.
- `internal/app`: composition root, dependency construction, and lifecycle.
- `internal/api/http`: HTTP adapter, service proxy, browser terminal, dashboard,
  access logging, and URL path parsing.
- `internal/api/control`: mTLS public control edge and raw gRPC proxy.
- `internal/api/tunnel`: public tunnel relay edge that forwards client peers to
  session-bound internal `tunneld` targets.
- `internal/api/ssh`: optional SSH-compatible terminal adapter.
- `internal/application/service`: service route resolve cache and endpoint
  rotation.
- `internal/application/terminal`: allocation terminal resolve/open use case.
- `internal/application/artifact`: ticket-backed artifact stream orchestration,
  range validation, limits, and backpressure.
- `internal/api/artifact`: public `ArtifactData` streaming adapter.
- `internal/adapters/artifact`: private controld ticket resolver and presigned
  object-store reader; it never owns object-store credentials.
- `internal/kernel`: narrow capability contracts shared across layers.
- `internal/adapters`: concrete `controld` and `axnoded` gRPC clients.
- `internal/config`, `internal/auth`, `internal/observability`: small support
  packages.

## Rules

- Preserve the dependency direction: `api -> application -> kernel <- adapters`;
  `internal/app` is the only composition root.
- Do not make application or kernel packages depend on API packages, app wiring,
  or concrete adapters.
- Keep protocol details in `internal/api`; keep use-case orchestration in
  `internal/application`; keep external gRPC clients in `internal/adapters`.
- Do not add compatibility layers, transitional aliases, `New...With...`
  constructor variants, or catch-all helper packages/files.
- If `internal/app` starts making behavioral decisions beyond construction and
  lifecycle, move that behavior into an application package.
- Keep route/lease caching and retry behavior with the owner of the behavior;
  do not hide it in generic utility packages.
- Artifact bytes flow through gatewayd. Never expose a presigned/internal URL,
  ticket, query, or Authorization value in responses, logs, metrics, or traces.
  Keep artifact IDs out of metric labels and close every upstream body on all
  client cancellation, short-read, and rejection paths.
- Use the dedicated `gatewayd` mTLS identity for internal controld calls. The
  private artifact resolver rejects the generic client and worker identities;
  do not weaken that boundary or reuse `client.crt` for gatewayd.

## Verification

From `gateway/gatewayd`:

```bash
go test ./...
go vet ./...
```

From the repo root:

```bash
make gatewayd-check-architecture
```

For integration-sensitive changes, also run the relevant smoke:

```bash
make local-compose-gateway-smoke
make local-compose-tunnel-e2e
```
