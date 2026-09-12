# Storage Architecture

Axern separates durable control state and delivered artifacts from
allocation-local writable files. It does not expose a generic Volume
Class/Claim/Binding product or run separate storage-control and volume-publish
daemons.

## Data Ownership And Lifetime

| Data | Owner and location | Lifetime |
| --- | --- | --- |
| Workload intent, allocations, attempts, placement, leases, resource reservations, and result metadata | `controld` and PostgreSQL | Durable control state; not a filesystem or process-output stream |
| Writable sandbox rootfs and TaskSet workspace | `axnoded` and node-local runtime filestore | One allocation; no persistence promise across allocation replacement or node loss |
| Immutable rootfs, read-only image bundles, and image caches | `imagemgr` and `imagefsd` where required | Image-owned cache and live mount leases, separate from writable workload data |
| Allocation ownership, cleanup intent, resource state, and recovery records | `axnoded` and its process-owned embedded database | Node-local recovery; not a second shared control-plane database |
| Delivered artifacts and rollout evidence | Existing artifact metadata and S3-compatible object storage | Explicit upload/export, ownership, and retention contracts |

PostgreSQL remains the only authoritative central state backend. Object storage
is a delivery and archive path, not a writable POSIX working directory. Removing
the generic volume product does not remove artifact object storage, deployment
database persistence, or node-runtime recovery records.

## Allocation-Local Filesystems

`axnoded` resolves an immutable image rootfs, prepares the allocation-private
writable view, and tracks runtime, image, and workspace ownership. Read-only
image mounts and runtime-owned bind mounts remain supported. Their target
validation, mount conflict checks, and cleanup are independent of the retired
volume API.

Writable rootfs storage still requires node-local reservation and runsc hard
enforcement. The current charged scope is the runsc file-backed root overlay,
including metadata, copy-up, and whiteouts; image caches, artifacts, logs, and
other uncharged classes do not silently become part of that reservation. See
[Resource Model](resource-model.md) and the
[node rootfs storage contract](../../runtime/axnoded/docs/rootfs-storage.md).

A replacement allocation starts from its immutable inputs, not from a previous
allocation's writable directory. A process restart may recover an existing
allocation when its runtime and ownership records are intact; this is not a
promise to retain files after node loss or allocation replacement. Export or
download required outputs before destroying the allocation. A successful file
download copies bytes to the caller; durable artifact publication remains a
separate explicit operation.

## Recovery And Cleanup

Allocation attempts, ownership, idempotent lifecycle operations, execution
leases, and fencing remain required. An expired control-plane lease does not
prove that a partitioned node's process has stopped.

Node cleanup stops the runtime and crosses the exit-state barrier before
releasing writable-rootfs and image ownership. Mount cleanup and writable
reservation release must complete before the associated resource commitment is
retired. Failed cleanup retains its ownership and retry state; it must not
advertise still-owned capacity as free. Node restart reconciles these records
against the runtime inventory without creating a persistent-volume attachment
workflow.

The preserved implementation boundaries are `runtime/axnoded/internal/nodestate`,
`internal/service/allocation`, and `internal/runtime/rootfsview`, with image
lease coordination in `internal/langruntime`. These are execution safety and
recovery mechanisms, not generic storage-provider abstractions.

## Historical Data And Upgrade Boundary

This is a coordinated control-plane, node, SDK, and deployment contract change.
Mixed-version operation and silent reinterpretation of old protobuf numbers or
persisted volume fields are unsupported.

Before adopting this model on an existing installation:

1. Use the archived matching release to inventory old claims, bindings,
   workloads, and physical directories. Export required data and verify the
   resulting copies separately from the source archive.
2. Drain the old allocations through that release and verify runtime, mount,
   and resource cleanup. Do not start the new node runtime over active
   historical allocation records.
3. Start matching new components with new clean control and node state. Keep
   historical database and filesystem data separately until an operator has
   explicitly approved its retention or disposal.

The current schema contains no persistent-volume tables or compatibility
checks. Development databases and node state are rebuilt when this execution
storage contract changes.

## Validation

- Host-safe checks cover lifecycle adapters, allocation ownership and cleanup,
  ephemeral reservations, node inventory, and strict configuration decoding.
- Linux truth checks cover runsc writable-rootfs enforcement, image/workspace
  mounts, restart recovery, execution, file transfer, and allocation cleanup.
- Deployment checks prove that the retained stack starts without either retired
  daemon. They must not invoke a historical volume cleanup workflow.
- Historical-data tests prove startup refusal is read-only, including
  tombstones and database read failures.

Use the [verification tiers](../verification/local-full-verification.md) and
[node verification matrix](../../runtime/axnoded/docs/verification.md) to select
the required checks. A source or host-safe check alone does not prove Linux
mount behavior or successful migration of an existing installation.
