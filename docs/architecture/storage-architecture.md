# Storage Architecture

Axern separates durable control state and delivered artifacts from allocation-local writable files. Persistent workspace orchestration is not part of the execution core.

## Data Ownership And Lifetime

| Data | Owner and location | Lifetime |
| --- | --- | --- |
| Workload intent, allocations, attempts, placement, leases, resource reservations, and result metadata | `controld` and PostgreSQL | Durable control state; not a filesystem or process-output stream |
| Writable sandbox rootfs and TaskSet workspace | `axnoded` and node-local runtime filestore | One allocation; no persistence promise across allocation replacement or node loss |
| Immutable rootfs, read-only image bundles, and image caches | `imagemgr` and `imagefsd` where required | Image-owned cache and live mount leases, separate from writable workload data |
| Allocation ownership, cleanup intent, resource state, and recovery records | `axnoded` and its process-owned embedded database | Node-local recovery; not a second shared control-plane database |
| Delivered artifacts and rollout evidence | Existing artifact metadata and S3-compatible object storage | Explicit upload/export, ownership, and retention contracts |

PostgreSQL is the only authoritative central state backend. Object storage is a delivery and archive path, not a writable POSIX working directory. Artifact storage, deployment database persistence, and node-runtime recovery records keep their distinct owners and lifetimes.

## Allocation-Local Filesystems

`axnoded` resolves an immutable image rootfs, prepares the allocation-private writable view, and tracks runtime, image, and workspace ownership. Read-only image mounts and runtime-owned bind mounts use the same target validation, conflict checks, and Allocation cleanup boundary.

Writable rootfs storage still requires node-local reservation and runsc hard enforcement. The current charged scope is the runsc file-backed root overlay, including metadata, copy-up, and whiteouts; image caches, artifacts, logs, and other uncharged classes do not silently become part of that reservation. See [Resource Model](resource-model.md) and the [node rootfs storage contract](../../runtime/axnoded/docs/rootfs-storage.md).

A replacement allocation starts from its immutable inputs, not from a previous allocation's writable directory. A process restart may recover an existing allocation when its runtime and ownership records are intact; this is not a promise to retain files after node loss or allocation replacement. Export or download required outputs before destroying the allocation. A successful file download copies bytes to the caller; durable artifact publication remains a separate explicit operation.

## Recovery And Cleanup

Allocation attempts, ownership, idempotent lifecycle operations, execution leases, and fencing remain required. An expired control-plane lease does not prove that a partitioned node's process has stopped.

Node cleanup stops the runtime and crosses the exit-state barrier before releasing writable-rootfs and image ownership. Mount cleanup and writable reservation release must complete before the associated resource commitment is released. Failed cleanup retains its ownership and retry state; it must not advertise still-owned capacity as free. Node restart reconciles these records against runtime inventory.

The preserved implementation boundaries are `runtime/axnoded/internal/nodestate`, `internal/service/allocation`, and `internal/runtime/rootfsview`, with image lease coordination in `internal/langruntime`. These are execution safety and recovery mechanisms, not generic storage-provider abstractions.

## Rebuild Boundary

During pre-stable development, protobuf, initial schema, node state, SDKs, and deployment manifests change as one coordinated contract. Rebuild the local PostgreSQL database and generated node state when that contract changes instead of adding dual schema paths or reinterpretation logic.

After Axern establishes a stable external storage contract, its production upgrade policy must define versioned backup, migration, rollback, and deprecation behavior explicitly.

## Validation

- Host-safe checks cover lifecycle adapters, allocation ownership and cleanup, ephemeral reservations, node inventory, and strict configuration decoding.
- Linux truth checks cover runsc writable-rootfs enforcement, image/workspace mounts, restart recovery, execution, file transfer, and allocation cleanup.
- Deployment checks prove that the complete execution stack starts with only its declared storage owners.
- Clean-rebuild tests prove that the initial schema and node state agree with the current execution-storage contract.

Use the [verification tiers](../verification/local-full-verification.md) and [node verification matrix](../../runtime/axnoded/docs/verification.md) to select the required checks. A source or host-safe check alone does not prove Linux mount behavior or a production-data upgrade contract.
