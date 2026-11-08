# Runtime Rootfs And Writable Storage Contract

Axern treats the lower rootfs as immutable input. It may be a directory, an existing OverlayFS view, or EROFS. Runtime behavior is selected from OCI semantics, the source-owned immutable mount descriptor, and runtime policy; it is not selected by an image-product type switch.

## Three independent boundaries

1. Host mount-target projection creates only missing bind targets before OCI create. Runsc receives it only when a target is missing.
2. Guest writable rootfs is a file-backed gVisor overlay.
3. The cgroup memory boundary accounts workload anonymous memory, shmem, runtime processes, kernel memory, EROFS lower page cache, file-backed writable-overlay page cache, dirty pages, and writeback. Filestore size and quota are storage controls and never substitute for memory enforcement.

## Runtime-provided system files

`/etc/hostname`, `/etc/hosts`, and `/etc/resolv.conf` form one OCI sandbox contract for runsc. Exact destinations, a parent destination, and `/` own their subtree; `/etc2` does not own `/etc`. An explicit owner suppresses the corresponding default source, mount, IP requirement, and resolver work.

Default hosts contain IPv4 and IPv6 localhost entries and map the effective hostname to the allocated sandbox IPv4. A missing or invalid interface IPv4 is a create error when the default hosts file is required. Sources live under the bundle-private `sandbox-files` directory. Sources and `config.json` use chmod/write/file-fsync/close/rename/parent-fsync atomic replacement. Axnoded never copies the node `/etc` tree and never creates targets in the lower rootfs.

## Immutable mount hand-off and projection

Imagemgr owns image representation, the effective immutable mount, its opaque identity, and its lease. Every image mount response returns one bounded, flat descriptor containing the effective root, filesystem diagnostics, exact ordered OverlayFS lower paths when projection needs them, readonly state, source-owned identity, and lease ID. OCI extraction and future filesystem representation details stop at this boundary. The projection provider validates and consumes the descriptor; it does not parse image metadata or reverse-engineer mountinfo to rediscover image layers. Local developer rootfs sources are described once by their source adapter using the same contract.

`projection.json` records this descriptor only for lifecycle correlation. Imagemgr and its lease reconciliation own lower health and identity changes; projection reconciliation owns the host OverlayFS mount, placeholder upper, and work directory. This prevents projection from becoming a second image manager. Unsafe or non-canonical lower paths and mount-option encoding are rejected before an OverlayFS mount.

Every bind source must be a regular file or directory. Destinations must be normalized absolute container paths. Parent and leaf symlinks, special files, non-directory parents, and source/target type mismatches are rejected. Missing parents copy mode/UID/GID from existing lower parents; genuinely new parents are root:root `0755`. Arbitrary xattrs, ACLs, devices, FIFOs, and sockets are never copied.

The projection upper root copies the effective immutable root's UID, GID and permission bits before mounting, so a private host creation umask cannot prevent guest users from traversing an otherwise accessible image. The view's enclosing directory, work directory and unmounted merge target remain private host artifacts. This copies guest metadata into the new Allocation's projection; it does not chmod image-cache roots or relax the service umask.

Artifacts are partitioned as `projections/<id>` and `runsc` under the filestore. The OCI readonly bit is preserved exactly. Imagemgr's active rootfs reference and immutable identity remain owned by the lower mount lease; projection cleanup must finish before that lease is released.

## Ephemeral-storage enforcement

Writable runsc roots must launch with exactly:

```text
--overlay2=root:dir=<filestore>/runsc,size=<resolved-limit-bytes>
```

There is no `root:memory`, direct-write, self-backing, or representation-based fallback. Ext4 and XFS can host runsc backing storage and target-only projections; neither requires project-quota enforcement.

The immutable runsc launch-enforcement manifest records the exact overlay argument, configured backing path, backing directory device/inode identity, runtime process identity, and filestore mount identity. Runtime verification rejects a symlink, directory replacement, changed immutable launch arguments, or identity mismatch even when a path with the same spelling still exists.

The node-local Allocation charge ledger is fsync/rename durable and checks both committed requests and live `statfs` availability after the system reserve. AllocationState, the Allocation charge ledger, the projection manifest, and runtime inventory provide restart inputs; OCI annotations are not an ownership or recovery source. Compressed EROFS copy-up is charged by actual upper usage; lower compressed size is not a capacity estimate.

## Private Secret Files

Resolved Secret files are materialized under the configured node root in `allocation-secrets/<allocation-key>`. The storage parent and Allocation directory remain node-owned `0700`; descriptor-relative operations reject symlinks, unsafe ownership, replaced directories and unexpected mounts. Guest target paths do not select host paths. Files preserve the explicitly granted read-only mode independently of the service umask and enter the sandbox through read-only bind projections.

Existing plaintext is never replaced by an admission replay. Runtime deletion must release its mounts before normal or failed-start cleanup removes the private files. A cleanup failure retains the Allocation admission for retry. Secret storage does not use a global temporary directory, and restart cleanup uses the same configured node root.

## Managed Workspace Storage

An optional Workspace bind at `/workspace` retains bytes independently of the disposable root overlay. The node consumes a typed, immutable control-plane binding containing Workspace/Namespace/Node identities, granted byte/inode limits, storage identity and writer generation. It rejects overlapping mounts or Secret-file projections and does not combine Workspace use with rootfs-snapshot publication. The node resolves the source from its private ownership records rather than accepting a public host path.

The managed root is an existing private XFS directory with project accounting and enforcement enabled. Provisioning assigns a unique project ID, inherited assignment, finite block and inode hard limits, and verifies kernel readback before availability is acknowledged. Trusted owner records live outside the guest-writable tree. Persistent identity uses the filesystem identity, owned inode and a durably recorded random owner epoch; current device and mount IDs fence each open operation without becoming a per-boot data identity. Reinitializing an empty root after metadata loss creates a new epoch and rejects every former storage grant; a nonempty root without its ownership record remains unavailable.

Attach, live enforcement audit and recovery verify the exact data inode, current root/mount relationship, inherited project assignment and quota limits. Missing or replaced data is unavailable and is never recreated by those operations. Writer generations remain fenced after release; cleanup intent also withdraws a delayed create that never reached node admission.

The normal runtime cleanup barrier stops Sentry and Gofer, removes runtime mounts/projections, retires image handles and resource claims, and atomically records the matching Workspace release receipt before discarding the Allocation admission record. A terminal workload or expired lease alone does not transfer the writer. Acknowledging an old receipt only prunes that receipt and cannot change a newer writer.

Explicit Workspace deletion requires a detached owner, durably records deletion intent, walks through pinned directory descriptors without following symlinks or crossing a mount, and confirms zero project block/inode use after filesystem synchronization. Open-unlinked files or uncertain cleanup retain the charge. Recursive deletion serializes only its own Workspace; unrelated live writer verification does not wait behind that tree walk.

Retired Workspace ownership records remain as delayed-command fences. The experimental profile does not qualify unlimited Workspace create/delete churn or automated metadata compaction; retained metadata requires planned capacity and an explicit maintenance contract.

## Readiness, observed capability, and cleanup

Filestore readiness requires a real OverlayFS probe and, when available, an EROFS copy-up probe using the production upper filesystem. Runtime-specific hard-limit capabilities require separate conformance sandboxes and per-Allocation verification. Provider ownership, evidence validity, and loss policy are defined in [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md); commands and coverage belong in [Verification](verification.md).

Cleanup crosses the runtime and monitor exit barrier before removing rootfs projections, writable state, Allocation charges, image leases, and finally the Allocation cgroup. Failure preserves every remaining owner for retry. Cgroup removal, not optional `memory.reclaim`, releases the memory commitment. Foreground runsc deletion sends `KILL`, waits for the persisted exit state and released sandbox lock, then performs forced deletion.

Environment-cache reference release is not proof of physical unmount. Managed image roots release through the complete desired lease set, not a parallel direct-unmount request. A released root remains non-acquirable in the cache until imagemgr confirms reconciliation; the existing sweeper retries failed delivery even when idle retention is disabled. It is removed after confirmation. This in-memory pending entry is not a second durable lease: after restart, complete Allocation recovery reconstructs the desired set, while imagemgr persists and retries accepted release intents. Reference underflow is rejected without changing the remaining ownership counts.

Startup collects one complete runsc inventory before destructive reconciliation. Unreadable inventory, conflicting identity, or incomplete potentially-live state fails closed. `created`, `running`, and `unknown` runtime states remain protected; terminal or proven-absent runtimes are cleaned in normal ownership order. Allocation identity comes from the manager-owned runtime ID and admitted AllocationState, never from `meta.pb`, OCI metadata, or name prefixes. Empty partial bundle shells may be removed only after runtime absence and storage cleanup are proven.
