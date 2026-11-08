# Managed Single-Writer Workspaces

## Status And Decision

This decision defines experimental managed Workspaces: independent identity and retained capacity, one fenced writer, and node-managed XFS project quotas. The implemented behavior is defined by [Storage Architecture](../architecture/storage-architecture.md). The creation protocol uses required admission RPCs; an older server rejects it before creating an ephemeral execution. Supported upgrades, cold backup and replacement-host restore remain unqualified; this feature does not promise durability against host or disk loss.

A Workspace is namespace-scoped retained data on one managed Linux host. PostgreSQL owns its identity, lifecycle, capacity charge and attachment authority; axnoded owns the bytes, enforced quota and mount observations. Environment remains immutable input, and execution remains `Environment -> Run -> Allocation`. Agent tasks, configuration, conversations, approvals and semantic retries stay in the caller.

The initial scope is one optional writable Workspace per Run, mounted at `/workspace`, with one attached Allocation at a time. There are no concurrent read-only attachments, cross-Namespace shares, arbitrary host paths or live multi-writer artifact directories. Common read-only inputs continue to use image mounts. Callers exchange selected results through explicit delivery and byte receipts, rather than acquiring a second writer to a source tree.

## Public Meaning And Ownership

| Object or fact | Meaning and owner |
| --- | --- |
| Workspace | Never-reused data identity, Namespace, bound Node, requested capacity, lifecycle and retained storage charge; authoritative in PostgreSQL |
| Attachment | Subordinate binding from one Workspace to one exact Allocation and generation; committed with Run admission |
| Managed bytes | A node-owned directory tree on the qualified persistent filesystem; independent of Allocation rootfs and image caches |
| Mount observation | Exact filesystem, directory, quota and attachment generation verified by axnoded before execution and recovery |
| Agent session | Caller-owned task and recovery context referencing Workspace and execution IDs; no Axern session state machine |

Public operations must cover create, get/list and explicit delete, together with Workspace selection at Run admission. Names are query conveniences, never authorization identities. Responses expose lifecycle, attached execution, retained capacity and explicit failure conditions; they do not expose host paths, quota identifiers, private owner markers or node credentials. Wire names, generated clients and CLI commands must be introduced together with the implementation rather than reserved by this decision.

An admitted Run freezes the authorized Workspace ID, bound Node, mount target and attachment generation. A new Run creates a new Allocation even when using the same Workspace. Old process, file, terminal, SSH and Tunnel handles remain bound to their original Allocation; a Workspace reference cannot redirect those handles to the replacement.

Workspace create/read/use/delete must participate in the existing Namespace authorization model. Namespace deletion rejects retained Workspaces, and Node retirement rejects persistent data or unresolved attachment obligations. Node revocation withdraws execution authority without claiming bytes were deleted or safely moved. Membership or credential revocation cannot transfer write ownership; the existing Run cancellation and execution-lease mechanisms must first stop the old execution.

## Provisioning And Retention

Provisioning starts with durable Workspace identity, Node binding, capacity charge and delivery intent in one PostgreSQL transaction. Node provisioning is idempotent for that exact identity. It creates a new private tree, applies and verifies its storage controls, writes a durable ownership marker and only then acknowledges availability. A failed or interrupted operation retains its charge and recoverable intent until its bytes and ownership are known.

The Workspace belongs to its original Node and qualified data filesystem. A missing, replaced, inaccessible or mismatched tree is unavailable data, not an empty Workspace. Provisioning may create bytes only for a new identity whose creation intent has never acknowledged existing data. Ordinary attach, restart, reconnect and restore paths must never create an absent retained tree. Node-local records are recovery projections of control intent and observed storage; they cannot grant another writer.

Stopping, cancelling or deleting a Run does not delete its Workspace. Normal full platform stop terminates processes and leaves retained bytes, metadata and charge intact. A daemon restart may take over an eligible runtime that is still alive on the same host boot. A host reboot ends those processes: confirm old runtime and attachment cleanup, converge the old Allocation and release its writer, then use a fresh Run and Allocation. Never restart a lost worker under its old execution ID. No conversation, process memory or external action is resumed merely because files remain.

## Admission And Writer Fencing

Run admission locks the Namespace, bound Node and selected Workspace in a consistent transaction order. It rechecks current authorization, data availability, storage enforcement capability and the absence of any active or ambiguous attachment. It atomically creates the Run, its single Allocation, ordinary execution charge, the exact Workspace attachment generation and node-create intent. Competing admissions cannot both acquire the same Workspace. A Workspace can constrain placement to its bound Node; it cannot trigger cross-host copying or rescheduling.

Before executing user code, axnoded verifies the frozen attachment against its managed directory and actual quota, then installs the sole managed bind at `/workspace`. A conflicting image mount, Secret projection, runtime mount or overlapping target is rejected. Failure leaves a visible failed execution and cleanup obligation; it does not fall back to an empty ephemeral directory.

The writer remains attached until the old runtime, its filesystem access through Sentry/Gofer and its mount ownership are confirmed absent and Allocation cleanup reaches `RELEASED`. Only the matching cleanup receipt and generation permit the control transaction to release the attachment. A terminal Run, lease expiry, missed heartbeat, RPC timeout or database flag alone is insufficient. Delayed create/delete/cleanup commands cannot act on a newer attachment generation.

Unknown cleanup retains the attachment and blocks the next writer. Recovery may retain an existing runtime only for the same Allocation, generation, directory identity and still-valid execution authority. Missing enforcement or storage identity terminates execution and retains the unresolved data obligation. There is no automatic transfer to another host when the original host cannot be fenced.

The bind uses the existing gVisor filesystem boundary. The first implementation keeps the normal shared-cache-coherence behavior for bind mounts; it does not enable exclusive filesystem caching while a host service could observe or change the data. [gVisor filesystem semantics](https://gvisor.dev/docs/user_guide/filesystem/) distinguish bind-mount access from its disposable rootfs overlay.

## Storage Enforcement And Accounting

The initial persistent storage profile uses a managed directory on XFS with project-quota accounting and enforcement enabled. This is an additional capability requirement for Workspace use, not a change to the existing ephemeral execution profile. Unsupported filesystems or missing quota enforcement reject Workspace provisioning and admission. There is no quota-free fallback, polling-only substitute or second storage backend.

Each Workspace receives a unique project identity and inherited project assignment, with finite block and inode hard limits. Provisioning verifies the effective controls before publishing availability; attach and runtime recovery revalidate them before allowing writes. Guest code must not be able to change project assignment or quota enforcement. The implementation must qualify subdirectories, hard links, sparse writes, open-but-unlinked files and exhaustion through the actual gVisor path. [XFS project quotas](https://www.kernel.org/pub/linux/utils/fs/xfs/docs/xfs_filesystem_structure.pdf) associate charge with filesystem metadata rather than a directory-size scan.

PostgreSQL charges the granted capacity while the Workspace exists, including detached, unavailable and deleting states. This charge is separate from Allocation ephemeral-rootfs charge and cannot be released by cancelling a Run. Namespace and Node admission use the same retained-capacity contract. Quota identifiers cannot be recycled while old data, an attachment or ambiguous cleanup can still reference them.

Quota limits are not a guarantee of reserved physical disk blocks. The installation must bound its persistent capacity budget, preserve operator storage reserve, and report filesystem exhaustion or failed writes explicitly; image caches and other host writers cannot be assumed harmless. Both capacity admission and actual full-disk behavior require qualification. Report granted block/inode limits and usage consistently, without adding hierarchical or overlapping values twice.

Host storage ancestors and owner records stay private. Ownership markers and trusted recovery metadata live outside the guest-writable mount tree; guest files cannot prove storage ownership. Guest-visible directory permissions and ownership are deliberate metadata, independent of the service umask. Managed-path operations must reject traversal, symlink substitution, unexpected mount/filesystem changes and cross-Workspace identity. Cleanup uses verified directory descriptors and never follows guest-created paths into another Workspace or the host. The public API cannot select a source host path.

## Deletion And Data Loss

Deletion is an explicit authorized Workspace operation. It rejects an active or ambiguous attachment and first commits deletion intent. The node verifies exact ownership and storage identity, removes only that Workspace's bytes, verifies that no charged files or mount access remain, and acknowledges durable deletion. Only then may PostgreSQL release retained capacity and complete deletion. Retries are idempotent; partial deletion retains charge and diagnosis.

An unavailable disk does not acknowledge deletion. Administrative force-forget, data-loss acknowledgment, quota reclamation and replacement-host recovery are outside the first public delete operation. They must not be smuggled into normal cleanup or inferred from a missing directory. Workspace IDs are never reused, and stale execution or deletion requests cannot acquire new authority.

## Cold Backup And Restore Boundary

Persistent APIs must not be advertised as durable until a consistent cold backup and isolated restore have passed. Rebuilding an initialized database or node state is not an upgrade or recovery procedure for retained work. Until a supported versioned maintenance contract exists, the persistent feature remains experimental and must reject incompatible metadata without altering retained data.

The cold backup boundary requires admission to be disabled and all workload, attachment, lifecycle and deletion operations to be drained or explicitly accounted as unresolved. No writer may remain active. Stop services through the managed lifecycle, synchronize data, and preserve the following set under one named installation/version boundary:

| Backup part | Required content |
| --- | --- |
| PostgreSQL | Namespace and authorization identities, Workspace metadata/charge/generation, Run/Allocation and lifecycle facts at the stopped boundary |
| Persistent filesystem | Workspace bytes and filesystem metadata, project assignments/limits, verified filesystem identity and node ownership records |
| Node recovery state | Matching node-local durable facts required to validate storage and reject stale execution; never independent permission to run |
| Configuration and keys | Installation configuration, CA/signing and encryption material, required service/node identity and administrator recovery procedure, stored as private material |
| Execution inputs | Exact runtime/archive/schema versions and immutable Environment image digests, with an explicit registry retention/access responsibility |

A database dump and an unrelated live directory copy are not this backup. Restore requires fencing the previous installation before granting any writer, verifying the complete set and quota metadata on an isolated host, and rejecting missing or conflicting parts. A reboot or restored lease record cannot revive old authority. New work must use a new Run/Allocation with explicitly established valid Node authority and the original immutable execution inputs. Recovery must demonstrate successful use of retained files and explicit failure for absent images, unavailable data or invalid keys; readable files alone do not prove restoration.

Ordinary attach and runtime recovery cannot change a Workspace's host or filesystem binding. Restoring copied bytes requires a separate, explicit administrative restore protocol: validate the backup manifest, fence the previous installation, invalidate restored execution/attachment authority, and verify the restored bytes and effective quota before committing the intended storage-identity mapping. Changed directory identities or filesystem UUIDs are accepted only by that controlled restore operation, never by a normal missing-directory fallback. A replacement host receives a new Node ID and credentials; copying the former host's recovery files or node key does not admit that identity on a different host.

The restore transaction must record the legitimate old-to-new Node and storage mapping and establish fresh attachment authority before admission resumes. Old Allocation IDs remain terminal, and stale commands cannot act on the restored data. The exact restore manifest, administrative transaction, authority invalidation and version compatibility must be implemented and qualified together. Until that protocol passes an isolated restore, this decision does not promise replacement-host recovery or durable Workspace availability.

## Coordinated Implementation And Qualification

Schema, protobuf, CLI, Go/Python/TypeScript SDKs, gateway authorization, placement, node lifecycle/recovery and documentation must ship as one implemented contract. An older component must reject a required persistent attachment rather than discard it. There is no public metadata-only Workspace, accepted-but-ignored mount, arbitrary host bind API or parallel Sandbox lifecycle.

Qualification must cover successful create/use/detach/recreate, concurrent writer admission, cancellation and partial cleanup, delayed commands, node restart and host reboot, missing/replaced storage, quota withdrawal and exhaustion, Namespace authorization, active deletion rejection, idempotent failed deletion and consistent isolated restore. A fresh Allocation must see receipt-verified retained files while the old Allocation's access remains denied. The complete Agent workflow is a consumer of these guarantees; Agent/provider compatibility is tested separately from storage correctness.

This decision narrows the first storage contract. Concurrent read-only access, live shared artifacts, resize, multi-host movement, distributed storage, process snapshots, automatic Git merging and Agent session management require independent demonstrated need and are not prerequisites for the single-writer path.
