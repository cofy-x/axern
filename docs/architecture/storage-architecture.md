# Storage Architecture

Axern separates durable control state from Allocation-local writable files. Persistent workspace and output-storage orchestration is not part of the execution core.

## Data Ownership And Lifetime

| Data | Owner and location | Lifetime |
| --- | --- | --- |
| Workload intent, declared-output specification, Allocations and their resource charges, placement, access grants, TunnelSessions, and Run result metadata | `controld` and PostgreSQL | Durable control state; contains no stdout or declared-output bytes |
| Writable sandbox rootfs and Allocation-local workspace | `axnoded` and node-local runtime filestore | One Allocation; no persistence promise across Allocation replacement or node loss |
| Sealed stdout/stderr and bounded declared outputs | `axnoded` node-local retention | Read-only through the Run output deadline; survives runtime cleanup and node-process restart, not node-disk loss |
| Published rootfs result | Operator-configured OCI repository; referenced by a derived Environment | Content-addressed and reusable across fresh Allocations; physical retention and unreferenced-blob GC are registry policy |
| Immutable rootfs, read-only image bundles, and image caches | `imagemgr` and `imagefsd` where required | Image-owned cache and live mount leases, separate from writable workload data |
| Allocation ownership, cleanup intent, resource state, and recovery records | `axnoded` and its process-owned embedded database | Node-local recovery; not a second shared control-plane database |

PostgreSQL is the only authoritative central state backend. Unselected bytes disappear with Allocation cleanup. Selected declared outputs and bounded logs are sealed before runtime deletion and remain readable only until the immutable output deadline. An explicitly requested successful rootfs result is instead published as an immutable OCI image and referenced by a normal Environment. Downloaded outputs belong to the caller or an upper-layer system. Object storage is never authoritative execution state or a writable POSIX working directory for an Allocation, and Axern does not define a generic public Artifact or Snapshot root.

## Allocation-Local Filesystems

`axnoded` resolves an immutable image rootfs, prepares the allocation-private writable view, and tracks runtime, image, and workspace ownership. Read-only image mounts and runtime-owned bind mounts use the same target validation, conflict checks, and Allocation cleanup boundary.

Writable rootfs storage still requires node-local Allocation charge and runsc hard enforcement. The charged scope is the runsc file-backed root overlay, including metadata, copy-up, and whiteouts; image caches, logs, and process output streams do not silently become part of that charge. See [Resource Model](resource-model.md) and the [node rootfs storage contract](../../runtime/axnoded/docs/rootfs-storage.md).

A replacement Allocation starts from immutable inputs, not from a previous writable directory. Process restart may recover an existing Allocation when its runtime and ownership records remain intact; this does not promise file retention after node loss or Allocation replacement. Declare bounded file or tar outputs before Run admission when a recovering caller must download them after termination; export them to caller-owned durable storage before expiry. The limits and ownership rules are defined in the [Stable Domain Model](../product/domain-model.md).

## Recovery And Cleanup

Allocation ownership, globally unique identities, idempotent lifecycle operations, execution leases, and exact-ID fencing remain required. The node treats an expired ExecutionLease as durable fail-closed termination intent and actively stops the sandbox; expiry alone is not evidence that cleanup has already completed.

Node cleanup quiesces the workload, captures declared outputs, syncs object bytes and the manifest, and atomically publishes the sealed output before deleting the runtime. For an explicitly requested successful rootfs result, cleanup then streams the stopped workload's runsc writable upper layer while the supervising sandbox still exists. Imagemgr validates paths and special files, converts gVisor whiteouts and opaque-directory metadata to OCI layer entries, enforces the bounded size, appends the layer to the immutable base, publishes a digest reference, and persists a minimal receipt before runtime deletion. A partial imagemgr staging file is never authoritative and is removed before retry or recovery. Mount cleanup and writable Allocation-charge release happen only after required barriers and must complete before the associated resource commitment is released. Failed cleanup retains its ownership and retry state; it must not advertise still-owned capacity as free. Node restart preserves a terminal runtime while rootfs sealing is pending and reconciles these records against runtime inventory.

Controld derives every rootfs-sealing command from the frozen Run and Environment specifications. The node requires an exact match with its immutable Allocation recovery copy and rejects missing, conflicting, or non-terminal state. After OCI publication, the node receipt makes Delete retry idempotent across crashes. Controld commits the derived Environment, immutable image metadata, Run snapshot result, and Allocation release in one transaction, then acknowledges the receipt. ExecutionLease expiry only stops the workload; it does not delete the runtime or bypass either sealing barrier.

The final control-plane delete carries a presence-bearing output-sealing command built only from the persisted Run specification and its immutable retention deadline. It is an ephemeral command, not another output authority. When node recovery state exists, its declared-output copy must match the command exactly; when that state is unexpectedly absent, the node publishes explicit `node_unavailable` entries instead of an empty success manifest. The sealed manifest stores only a deterministic contract digest so retries cannot change paths, formats, media types, ordering, or the explicit zero-output contract. That digest is an integrity check, never an Allocation identity.

The preserved implementation boundaries are `runtime/axnoded/internal/nodestate`, `internal/service/allocation`, and `internal/runtime/rootfsview`, with image lease coordination in `internal/environmentcache`. These are execution safety and recovery mechanisms, not generic storage-provider abstractions.

## Rebuild Boundary

During pre-stable development, protobuf, initial schema, node state, SDKs, and deployment manifests change as one coordinated contract. Rebuild the local PostgreSQL database and generated node state when that contract changes instead of adding dual schema paths or reinterpretation logic.

After Axern establishes a stable external storage contract, its production upgrade policy must define versioned backup, migration, rollback, and deprecation behavior explicitly.

## Validation

Host-safe checks cover ownership, cleanup, resource accounting, inventory, and configuration. Linux truth checks cover runsc enforcement, mounts, restart recovery, file transfer, and cleanup; clean-rebuild checks keep central and node-local state aligned. Use the [verification tiers](../verification/local-full-verification.md) and [node verification matrix](../../runtime/axnoded/docs/verification.md) to select the required checks.
