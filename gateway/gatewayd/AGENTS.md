# gatewayd Agent Contract

## Purpose

`gatewayd` is Axern's external control and data-plane gateway. It does not own
placement, lifecycle, or durable state. It proxies public control APIs to
`controld`, resolves allocation targets and terminal leases through `controld`,
and forwards allocation-bound data-plane traffic to `axnoded` or `tunneld`.

## Layout

- `main.go`: process bootstrap only.
- `internal/app`: composition root, dependency construction, and lifecycle.
- `internal/api/http`: HTTP adapter, browser terminal, access logging, and URL
  path parsing. It must not restore `/svc` routing or a generic Service proxy.
- `internal/api/control`: mTLS public control edge and raw gRPC proxy.
- `internal/api/tunnel`: public tunnel relay edge that forwards client peers to
  session-bound internal `tunneld` targets.
- `internal/api/ssh`: optional SSH-compatible terminal adapter.
- `internal/application/artifact`: allocation artifact transfer orchestration.
- `internal/application/terminal`: allocation terminal resolve/open use case.
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
- Use the dedicated `gatewayd` mTLS identity for internal controld calls; do
  not reuse the external client certificate for gateway-to-control traffic.
- Terminal, SSH, tunnel, artifact, process, and file paths must resolve an
  explicit Allocation and honor its attempt-scoped authorization. Do not infer
  a target from Service identity, replicas, or a persistent route.

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
make local-compose-server-base-smoke
make local-compose-python-sdk-e2e
```
