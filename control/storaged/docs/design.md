# Axern Storage V1 Design

Storage V1 is Axern's stateful service storage contract. It keeps durable
storage intent in `storaged`, service placement and lifecycle in `controld`,
and physical node publish work in `volumed` through `axnoded`.

The cross-service lifecycle and ownership contract is canonical in
[Storage Architecture](../../../docs/architecture/storage-architecture.md).
This document records the Storage V1 domain model and protocol standards owned
by `control/storaged`.

## Scope

Goals:

- Provide a stable storage model for stateful `Service` workloads.
- Preserve node-local service volumes as the first provider.
- Make topology, runtime compatibility, backend capability, health, and
  operator repair explicit before adding more providers.
- Leave room for Kubernetes PVCs and cloud CSI-backed storage without making
  Axern itself a CSI implementation.

Non-goals for the current foundation phase:

- NAS/NFS, Kubernetes PVC, or object-store-backed providers.
- Snapshots, backups, restore, resize, or quota.
- `Run` volumes.
- Direct `storaged -> axnoded` runtime calls.

## Domain Model

- `VolumeClass`: backend capability and policy defaults. The current committed
  class is `local`.
- `VolumeClaim`: durable user intent for a reusable service data volume.
- `VolumeBinding`: one allocation's control-plane attachment to a claim,
  workload, backend identity, topology, and selected node.
- `VolumeMount`: service-level mount intent, expressed as claim name, target,
  read-only flag, and mount options.
- `ResolvedNodeVolume`: the node publish spec returned by `storaged` to
  `controld` after placement selects a node.
- `PublishedNodeVolume`: the host path and mount options produced by `volumed`
  and reported back through `axnoded` and `controld`.

`storaged` owns classes, claims, bindings, resolved specs, publish/release
observations, binding health, and failed-binding retry. It does not own OCI
bundle creation, runtime mounts, node-local provider execution, or workload
public APIs.

## Capability Standards

Access modes:

- `ReadWriteOnce`: one writer on one node or topology domain.
- `ReadOnlyMany`: many readers.
- `ReadWriteMany`: only for providers with shared filesystem semantics.

Attachment scopes:

- `Service`: data persists across service allocation replacement.
- `Allocation`: data exists only for one allocation.
- `External`: Axern references data managed outside Axern.

Reclaim policies:

- `Retain`: deleting the claim keeps backend data.
- `Delete`: deleting the claim enters durable physical reclaim and reaches the
  terminal state only after provider cleanup is observed.

Consistency profiles:

- `POSIX`: normal single-volume filesystem expectations.
- `SharedFilesystem`: shared file semantics across clients.
- `ObjectStore`: weaker object-store semantics, not a general read-write POSIX
  filesystem.
- `Cache`: reconstructable or discardable data.

Topology keys:

- `node_id`: node-local storage.
- `zone`: zone-bound cloud storage.
- `cluster`: cluster-wide shared storage.

Every provider must declare runtime compatibility for `runc`, `runsc`, or both.
FUSE, device-backed mounts, mount propagation, and filesystem notification
behavior can differ under `runsc`, so compatibility is part of the protocol.

## Lifecycle Rules

- Claim status summarizes durable data intent across active bindings.
- Binding status records one allocation's publish or release observation.
- Allocation release leaves a service-scoped claim reusable and must not delete
  service data.
- Claim deletion is the only path that evaluates reclaim policy.
- A `Delete` Claim persists reclaim attempt, lease, next retry, topology, and
  redacted error state. Node or controller outages do not cancel deletion.
- Reclaim selection and lease expiry use the PostgreSQL clock. Multiple
  storaged and controld replicas claim with `SKIP LOCKED`; completion is fenced
  by lease owner, opaque token hash, generation, and expiry.
- Claim ID is backend identity. Deleted tombstones remain auditable while the
  active namespace/name uniqueness rule permits recreation with a new ID.
- Failed bindings are not terminal. Retry resets a failed binding to `Bound`,
  clears stale publish state, records an admin audit event through `controld`,
  and lets the normal `controld -> axnoded -> volumed` publish path run again.
- Publish and release observations are validated before they mutate state.
  Published observations require a `PublishedNodeVolume`; failed observations
  require a message that can be surfaced through admin storage and workload
  diagnostics.
- Release reporting is idempotent only for replaying the same deleted
  observation after a binding is already deleted. A late failed release
  observation against a terminal binding remains an operator-visible error.
- Binding health comes from `storaged`; admin reliability should aggregate that
  health instead of deriving binding state from node-local runtime records.

## Foundation Status

The foundation phase is closed around the local provider truth path:

- Public Storage V1 and private StorageCoordinator protocols.
- Runnable `storaged` daemon and Postgres-backed class, claim, and binding
  state.
- Local provider publish/unpublish/delete through `runtime/volumed` and
  `axnoded`, with Claim-owned physical paths.
- Startup reconcile, release idempotency, binding consistency counters, admin
  reliability integration, and `admin storage list/retry/reclaim`.
- A dedicated bounded controld reclaim dispatcher with per-node isolation and
  durable queue health metrics; Service reconciliation only persists deletion
  intent and observes completion.
- Compose and kind service-volume smoke coverage through
  `make local-storage-verify`.

Future Kubernetes PVC, NAS/NFS, and object-store-backed dataset/cache providers
should reuse this protocol only after their product contracts are concrete.
