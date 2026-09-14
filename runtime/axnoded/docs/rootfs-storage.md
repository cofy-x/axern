# Runtime Rootfs And Writable Storage Contract

Axern treats the lower rootfs as immutable input. It may be a directory, an existing OverlayFS view, Nydus, or EROFS. Runtime behavior is selected from OCI semantics, the source-owned immutable mount descriptor, and runtime policy; it is not selected by an image-product type switch.

## Three independent boundaries

1. Host mount-target projection creates only missing bind targets before OCI create. Runsc receives it only when a target is missing.
2. Guest writable rootfs is a file-backed gVisor overlay.
3. The cgroup memory boundary accounts workload anonymous memory, shmem, runtime processes, kernel memory, EROFS lower page cache, file-backed writable-overlay page cache, dirty pages, and writeback. Filestore size and quota are storage controls and never substitute for memory enforcement.

## Runtime-provided system files

`/etc/hostname`, `/etc/hosts`, and `/etc/resolv.conf` form one OCI sandbox contract for runsc. Exact destinations, a parent destination, and `/` own their subtree; `/etc2` does not own `/etc`. An explicit owner suppresses the corresponding default source, mount, IP requirement, and resolver work.

Default hosts contain IPv4 and IPv6 localhost entries and map the effective hostname to the allocated sandbox IPv4. A missing or invalid interface IPv4 is a create error when the default hosts file is required. Sources live under the bundle-private `sandbox-files` directory. Sources and `config.json` use chmod/write/file-fsync/close/rename/parent-fsync atomic replacement. Axnoded never copies the node `/etc` tree and never creates targets in the lower rootfs.

## Immutable mount hand-off and projection

Imagemgr owns image representation, the effective immutable mount, its opaque identity, and its lease. Every image mount response returns one bounded, flat descriptor containing the effective root, filesystem diagnostics, exact ordered OverlayFS lower paths when projection needs them, readonly state, source-owned identity, and lease ID. OCI, Nydus, and future EROFS details stop at this boundary. The projection provider validates and consumes the descriptor; it does not parse image metadata or reverse-engineer mountinfo to rediscover image layers. Local developer rootfs sources are described once by their source adapter using the same contract.

`projection.json` records this descriptor only for lifecycle correlation. Imagemgr and its lease reconciliation own lower health and identity changes; projection reconciliation owns the host OverlayFS mount, placeholder upper, and work directory. This prevents projection from becoming a second image manager. Unsafe or non-canonical lower paths and mount-option encoding are rejected before an OverlayFS mount.

Every bind source must be a regular file or directory. Destinations must be normalized absolute container paths. Parent and leaf symlinks, special files, non-directory parents, and source/target type mismatches are rejected. Missing parents copy mode/UID/GID from existing lower parents; genuinely new parents are root:root `0755`. Arbitrary xattrs, ACLs, devices, FIFOs, and sockets are never copied.

Artifacts are partitioned as `projections/<id>` and `runsc` under the filestore. The OCI readonly bit is preserved exactly. Imagemgr's active rootfs reference and immutable identity remain owned by the lower mount lease; projection cleanup must finish before that lease is released.

## Ephemeral-storage enforcement

Writable runsc roots must launch with exactly:

```text
--overlay2=root:dir=<filestore>/runsc,size=<resolved-limit-bytes>
```

There is no `root:memory`, direct-write, self-backing, or representation-based fallback. Ext4 and XFS can host runsc backing storage and target-only projections; neither requires project-quota enforcement.

The immutable runsc launch-enforcement manifest records the exact overlay argument, configured backing path, backing directory device/inode identity, runtime process identity, and filestore mount identity. Runtime verification rejects a symlink, directory replacement, changed immutable launch arguments, or identity mismatch even when a path with the same spelling still exists.

The node-local Allocation charge ledger is fsync/rename durable and checks both committed requests and live `statfs` availability after the system reserve. AllocationState, the Allocation charge ledger, the projection manifest, and runtime inventory provide restart inputs; OCI annotations are not an ownership or recovery source. Compressed EROFS copy-up is charged by actual upper usage; lower compressed size is not a capacity estimate.

## Readiness, observed capability, and cleanup

Filestore readiness requires a real OverlayFS probe and, when available, an EROFS copy-up probe using the production upper filesystem. Runtime-specific hard-limit capabilities require separate conformance sandboxes and per-Allocation verification. Provider ownership, evidence validity, and loss policy are defined in [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md); commands and coverage belong in [Verification](verification.md).

Cleanup crosses the runtime and monitor exit barrier before removing rootfs projections, writable state, Allocation charges, image leases, and finally the Allocation cgroup. Failure preserves every remaining owner for retry. Cgroup removal, not optional `memory.reclaim`, releases the memory commitment. Foreground runsc deletion sends `KILL`, waits for the persisted exit state and released sandbox lock, then performs forced deletion.

Startup collects one complete runsc inventory before destructive reconciliation. Unreadable inventory, conflicting identity, or incomplete potentially-live state fails closed. `created`, `running`, and `unknown` runtime states remain protected; terminal or proven-absent runtimes are cleaned in normal ownership order. Allocation identity comes from the manager-owned runtime ID and admitted AllocationState, never from `meta.pb`, OCI metadata, or name prefixes. Empty partial bundle shells may be removed only after runtime absence and storage cleanup are proven.
