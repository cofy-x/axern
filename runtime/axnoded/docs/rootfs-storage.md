# Runtime Rootfs And Writable Storage Contract

Axern treats the lower rootfs as immutable input. It may be a directory, an
existing OverlayFS view, Nydus, or EROFS. Runtime behavior is selected from OCI
semantics, observed mount facts, and the runtime policy; it is not selected by
an image-product type switch.

## Three independent boundaries

1. Host mount-target projection creates only missing bind targets before OCI
   create. Runsc receives it only when a target is missing. Runc receives it
   when a target is missing or whenever the OCI root is writable.
2. Guest writable rootfs is a file-backed gVisor overlay for runsc and a
   sandbox-private host OverlayFS plus XFS project quota for runc.
3. The cgroup memory boundary accounts workload anonymous memory, shmem,
   runtime processes, and file-backed page cache. Filestore size and quota are
   storage controls and never substitute for memory enforcement.

## Runtime-provided system files

`/etc/hostname`, `/etc/hosts`, and `/etc/resolv.conf` form one OCI sandbox
contract for runc and runsc. Exact destinations, a parent destination, and `/`
own their subtree; `/etc2` does not own `/etc`. An explicit owner suppresses
the corresponding default source, mount, IP requirement, and resolver work.

Default hosts contain IPv4 and IPv6 localhost entries and map the effective
hostname to the allocated sandbox IPv4. A missing or invalid interface IPv4 is
a create error when the default hosts file is required. Sources live under the
bundle-private `sandbox-files` directory. Sources and `config.json` use
chmod/write/file-fsync/close/rename/parent-fsync atomic replacement. Axnoded
never copies the node `/etc` tree and never creates targets in the lower rootfs.

## Projection and backing facts

The provider inspects `/proc/self/mountinfo`, records the deepest covering mount
ID, effective root path, filesystem type, mount root, source, readonly state,
and the path plus mount identity of every ordered effective lower, and persists those facts in
`projection.json`. Reconciliation re-inspects that same effective root and
requires all fields, including readonly state, lower ordering, and per-lower
mount identity, to match. EROFS is one atomic immutable lower. For OverlayFS,
the active upper is placed before lowerdirs and
mount-root offsets are preserved. Unsafe mountinfo or overlay-option encoding
is rejected.

Every bind source must be a regular file or directory. Destinations must be
normalized absolute container paths. Parent and leaf symlinks, special files,
non-directory parents, and source/target type mismatches are rejected. Missing
parents copy mode/UID/GID from existing lower parents; genuinely new parents
are root:root `0755`. Arbitrary xattrs, ACLs, devices, FIFOs, and sockets are
never copied.

Artifacts are partitioned as `projections/<id>`, `runc/<id>`, and `runsc` under
the filestore. The OCI readonly bit is preserved exactly. Imagemgr's active
rootfs reference is the lower mount lease; its lease ID is also recorded in the
projection manifest for reconciliation.

## Ephemeral-storage enforcement

Writable runsc roots must launch with exactly:

```text
--overlay2=root:dir=<filestore>/runsc,size=<resolved-limit-bytes>
```

There is no `root:memory`, direct-write, self-backing, or representation-based
fallback. Writable runc roots always use a host OverlayFS and require a durable
project ID plus an XFS project hard quota. Ext4 can host runsc and target-only
projections but cannot satisfy the runc ephemeral-storage hard-limit
capability.

The immutable runsc launch-enforcement manifest records the exact overlay
argument, configured backing path, backing directory device/inode identity,
runtime process identity, and filestore mount identity. Runtime verification
rejects a symlink, directory replacement, changed immutable launch arguments, or
identity mismatch even when a path with the same spelling still exists. The
runc manifest similarly binds the upper project ID, quota limit, OverlayFS
projection, and filestore mount identity and verifies them through the kernel.

The node-local reservation ledger is fsync/rename durable and checks both
committed requests and live `statfs` availability after the system reserve.
The reservation, limit, runtime, project ID, and OCI annotation are available
to restart reconciliation. Compressed EROFS copy-up is charged by actual upper
usage; lower compressed size is not a capacity estimate.

## Readiness, observed capability, and cleanup

Filestore startup performs a real OverlayFS scratch mount and XFS project-quota
probe. If an EROFS fixture is installed, it mounts the real image and exercises
read, copy-up, create, whiteout, and directory operations using the production
upper filesystem. Only successful probes can support the corresponding derived
platform capability. Runtime-specific memory and ephemeral-storage hard-limit
capabilities additionally require separate local conformance sandboxes so the
cgroup and storage boundaries cannot mask one another; each real allocation is
verified again. Provider ownership, evidence validity, and loss policy are
defined in
[Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md).

Cleanup order is runtime delete, projection/host-overlay unmount, upper/work
removal, writable reservation/project-ID release, then image mount lease
release. If runtime delete fails and the process may still live, the projection,
reservation, project ID, and lower lease remain for reconciliation.
For foreground `runsc run` sandboxes, forced deletion is an ordered runtime
protocol: send `KILL`, wait until the runtime runner has persisted exit state
and released the sandbox lock, then execute `runsc delete --force`. Issuing
delete before that barrier can deadlock teardown between the deleting process
and the still-converging foreground command.

At daemon startup, one complete generation of successful inventories from all
enabled runc/runsc handlers is the sole liveness authority for runtime-private
projections, writable reservations, allocation recovery records, and container
resource claims. Persisted metadata is recovery input, but cannot keep storage
alive after the owning runtime has disappeared. Inventory collection and
ownership validation finish before the first cleanup; an unreadable inventory,
duplicate ownership, missing potentially-live metadata, or an unknown persisted
runtime causes fail-closed retention with no partial deletion. If a listed
runtime is still present, backing identity and hard-limit state remain
fail-closed.
Container recovery identity comes from the metadata stored in the
manager-owned container directory; allocation IDs are not required to use an
axnoded-generated prefix. A `created`, `running`, or `unknown` runtime state is
retained fail-closed. A `stopped` state is terminal rather than live: startup
force-deletes that OCI runtime record first, then cleans projection/reservation,
allocation state, resource claims, and bundle metadata in the normal ownership
order. Once runtime absence or terminal deletion is proven, reconciliation also
enumerates manager-owned bundle directories directly so a missing `meta.pb`
cannot leak an otherwise recoverable OCI spec and its resource claims. If both
metadata and `config.json` are absent after runtime absence and storage cleanup
have been proven, the remaining directory is an empty/partial bundle shell and
is removed without inventing resource ownership.
