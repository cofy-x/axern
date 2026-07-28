# volumed

`runtime/volumed` is Axern's node-local volume daemon. It owns physical storage
publish, unpublish, state recovery, and backend-specific mount preparation for
volumes that have already been resolved by the control plane.

This README focuses on the `volumed` process. The cross-service ownership
contract is maintained in
[Storage Architecture](../../docs/architecture/storage-architecture.md).

## Interfaces

- Default Unix socket: `/run/volumed/volumed.sock`
- Default state root: `/var/lib/volumed`
- Default local volume root: `/var/lib/volumed/local`
- Devbox Unix socket: `.dev/run/volumed.sock`
- Devbox state root: `.dev/volumed`

The private gRPC API is `axern.private.runtime.volume.v1.RuntimeVolumeService`:

- `PublishVolume`
- `UnpublishVolume`
- `DeleteVolume`
- `GetPublishedVolume`
- `ListPublishedVolumes`
- `ReconcileVolumes`
- `GetVolumeManagerHealth`

`PublishVolume` accepts an already resolved
`axern.private.storage.v1.ResolvedNodeVolume` and returns a `PublishedVolume`
with the prepared host path and OCI mount options. `axnoded` maps that result
into the node lifecycle response; `controld` reports the publish or release
observation back to `storaged`.

`UnpublishVolume` returns the volumes it released so `axnoded` can report
binding-level release observations through the normal node lifecycle response.
`ReconcileVolumes` receives the active allocation ids from `axnoded` startup,
validates persisted provider state, and unpublishes records for allocations
that are no longer active on the node. It is a node-local cleanup path and does
not change placement or claim ownership. Cleanup runs without holding the
manager state lock during provider calls, and persisted records are removed
only when they still match the reconcile snapshot. This preserves fresh publish
state if an allocation is recreated while stale cleanup is in progress.

`DeleteVolume` accepts only a structured backend, Claim ID, and matching
backend handle. The local provider refuses published Claims, path traversal,
root deletion, and symlink targets. Missing directories are an idempotent
success. Local provider directories are Claim-owned at
`<local-volume-root>/<claim-id>`.

`GetVolumeManagerHealth` reports published volume count and the last reconcile
result. `axnoded` folds that into node summaries, and `controld` exposes
unhealthy volume managers through admin reliability without making `volumed` a
control-plane participant.

## Current Providers

- `local`: Claim-owned node-local directory under the configured local volume
  root.

Future providers should live behind the same provider interface. NFS/NAS,
Kubernetes PVC path discovery, CSI bridge integrations, and object-store-backed
dataset/cache providers should be added only when their runtime contract is
concrete enough to preserve the same resolved-spec-to-published-volume flow.
Every provider must declare capabilities before it is registered: backend,
supported access modes, consistency profiles, and runtime compatibility for
`runc`, `runsc`, or both. `volumed` validates resolved volume specs against
those capabilities before calling provider publish.

In `node-all-in-one` deployments, `volumed` stores local provider data under
the node state host path. Helm defaults that root to `/var/lib/axern/node`, so
operators should treat it as durable node-runtime state.

## Run Example

```bash
go run ./cmd/volumed \
  -root /tmp/volumed \
  -socket /tmp/volumed.sock \
  -local-root /tmp/volumed/local
```

## Build And Test

```bash
make build
make test
make vet
```

For cross-runtime volume changes, also run the `runtime/axnoded` host tests and
the compose/kind service volume smoke checks from the repository root.
