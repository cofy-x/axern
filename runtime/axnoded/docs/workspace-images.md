# Workspace Images

`ExecutionConfig.workspace_image` is a dedicated TaskSet workspace primitive.
It does not change the semantics of read-only `image_mounts` used for agents
and tools.

The contract contains ordered `variants` (`nydus`, then `oci`), a payload
`source_path` such as `tasks/<task-id>/workspace`, and an absolute target
(the TaskSet compiler uses the task workdir, normally `/workspace`). Axnoded attempts variants in order, retains the chosen
imagefs root, validates the source subtree, and creates an allocation-local
overlay with separate upper/work/merged directories under `filestore_dir`.
After the overlay mount, Axnoded performs one root metadata copy-up and fixes
the merged workspace root at mode `0777`. This lets mounted agents run as an
arbitrary numeric non-root user and create top-level outputs without a
rootfs-specific named account. The operation is O(1): Axnoded never recursively
changes ownership or permissions and never copies the lower workspace tree.

Every task and attempt receives a distinct upperdir. Cancellation, failed
start, and delete paths unmount the overlay and release the active image ref.
Asset materialization is serialized with allocation start/delete lifecycle so
cleanup cannot unmount a workspace while verifier or oracle files are copied.
Verifier and oracle prefixes are not mounted. They can only be copied into the
active COW workspace through the lease-authenticated `MaterializeTaskAssets`
RPC, which enforces task prefix, asset kind, target boundary, file type,
symlink-safe destination components, and no-overwrite rules.
