# storaged

`storaged` is Axern's control-plane storage service for stateful workloads.

This directory contains the Storage V1 design, protocol implementation,
Postgres-backed state store, and runnable daemon.

## Platform Role

`storaged` owns Storage V1 domain state:

- Volume classes, claims, allocation bindings, backend identity, topology, and
  resolved node volume specs.
- Publish and release observations reported by `controld` after node lifecycle
  work completes.
- Binding health and failed-binding retry for operator and reliability
  surfaces.
- Durable Claim deletion intent, reclaim leases, retry backoff, and tombstones.
- Database-claimed physical reclaim work with DB-clock leases and fenced worker
  completion.

`storaged` does not own:

- Public workload APIs.
- OCI bundle creation, runtime mounts, cgroups, or sandbox lifecycle.
- Node-local physical volume publish/unpublish/delete; that belongs to
  `volumed` and is dispatched through `controld` and `axnoded`.
- Image rootfs mounting; that belongs to `imagemgr` and `imagefsd`.

## Backend Scope

The current foundation phase supports the `local` provider truth path:
service-scoped node-local directories published by `volumed`. Kubernetes PVC,
NAS/NFS, and object-store-backed providers are intentionally deferred until
their product contracts are concrete.

## Protocols

Shared protocol drafts live in:

- [`../../sdk/proto/axern/control/storage/v1`](../../sdk/proto/axern/control/storage/v1):
  public Storage V1 API shape.
- [`../../sdk/proto/axern/private/storage/v1`](../../sdk/proto/axern/private/storage/v1):
  repo-internal storage coordination shape for `controld`, `storaged`, and
  `axnoded`.

See [Storage Design](docs/design.md) for the Storage V1 domain and protocol
standards. The canonical cross-service ownership and lifecycle model lives in
[Storage Architecture](../../docs/architecture/storage-architecture.md).

## Run

```bash
go run ./cmd/storaged \
  -grpc-address 127.0.0.1:24020 \
  -http-address 127.0.0.1:24021 \
  -postgres-dsn 'postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable'
```

Physical reclaim is a durable PostgreSQL queue. Claim selection is bounded and
uses `FOR UPDATE SKIP LOCKED`; active bindings are excluded in the same query.
Only a matching, unexpired lease owner, opaque token hash, and generation may
commit success or retry state. Lease tokens are returned only to the internal
worker and are never persisted in the public Claim payload. Queue health reports
due, scheduled, active-leased, expired-leased, and oldest-due state.
