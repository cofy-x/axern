# Storage Architecture

Axern separates durable control state from Allocation-local writable files. Persistent workspace and output-storage orchestration is not part of the execution core.

## Data Ownership And Lifetime

| Data | Owner and location | Lifetime |
| --- | --- | --- |
| Workload intent, allocations, placement, leases, resource reservations, and result metadata | `controld` and PostgreSQL | Durable control state; not a filesystem or process-output stream |
| Writable sandbox rootfs and Allocation-local workspace | `axnoded` and node-local runtime filestore | One Allocation; no persistence promise across Allocation replacement or node loss |
| Immutable rootfs, read-only image bundles, and image caches | `imagemgr` and `imagefsd` where required | Image-owned cache and live mount leases, separate from writable workload data |
| Allocation ownership, cleanup intent, resource state, and recovery records | `axnoded` and its process-owned embedded database | Node-local recovery; not a second shared control-plane database |

PostgreSQL is the only authoritative central state backend. Downloaded outputs belong to the caller or an upper-layer system; object storage is never authoritative execution state or a writable POSIX working directory for an Allocation. Axern does not currently define a generic public Artifact root.

## Allocation-Local Filesystems

`axnoded` resolves an immutable image rootfs, prepares the allocation-private writable view, and tracks runtime, image, and workspace ownership. Read-only image mounts and runtime-owned bind mounts use the same target validation, conflict checks, and Allocation cleanup boundary.

Writable rootfs storage still requires node-local reservation and runsc hard enforcement. The charged scope is the runsc file-backed root overlay, including metadata, copy-up, and whiteouts; image caches, logs, and process output streams do not silently become part of that reservation. See [Resource Model](resource-model.md) and the [node rootfs storage contract](../../runtime/axnoded/docs/rootfs-storage.md).

A replacement Allocation starts from immutable inputs, not from a previous writable directory. Process restart may recover an existing Allocation when its runtime and ownership records remain intact; this does not promise file retention after node loss or Allocation replacement. Export required outputs before destroying the Allocation. Any future durable output API must follow the ownership rules in the [Stable Domain Model](../product/domain-model.md).

## Recovery And Cleanup

Allocation ownership, globally unique identities, idempotent lifecycle operations, execution leases, and exact-ID fencing remain required. An expired control-plane lease does not prove that a partitioned node's process has stopped.

Node cleanup stops the runtime and crosses the exit-state barrier before releasing writable-rootfs and image ownership. Mount cleanup and writable reservation release must complete before the associated resource commitment is released. Failed cleanup retains its ownership and retry state; it must not advertise still-owned capacity as free. Node restart reconciles these records against runtime inventory.

The preserved implementation boundaries are `runtime/axnoded/internal/nodestate`, `internal/service/allocation`, and `internal/runtime/rootfsview`, with image lease coordination in `internal/langruntime`. These are execution safety and recovery mechanisms, not generic storage-provider abstractions.

## Rebuild Boundary

During pre-stable development, protobuf, initial schema, node state, SDKs, and deployment manifests change as one coordinated contract. Rebuild the local PostgreSQL database and generated node state when that contract changes instead of adding dual schema paths or reinterpretation logic.

After Axern establishes a stable external storage contract, its production upgrade policy must define versioned backup, migration, rollback, and deprecation behavior explicitly.

## Validation

Host-safe checks cover ownership, cleanup, reservations, inventory, and configuration. Linux truth checks cover runsc enforcement, mounts, restart recovery, file transfer, and cleanup; clean-rebuild checks keep central and node-local state aligned. Use the [verification tiers](../verification/local-full-verification.md) and [node verification matrix](../../runtime/axnoded/docs/verification.md) to select the required checks.
