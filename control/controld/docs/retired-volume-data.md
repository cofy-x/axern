# Retired Persistent-Volume Data

Generic Volume Class, Claim, Binding, mount, and physical reclaim APIs are not
part of the execution core. Sandbox-lifetime writable rootfs and workspace
preparation remain allocation-owned. Uploaded artifacts remain separately
owned durable results. Neither path promises equivalent persistence for an old
node-local volume.

## Upgrade Boundary

This release requires a coordinated control-plane, node, and SDK deployment.
Do not attach it to a running old stack or mix protobuf revisions. Persisted
protobuf JSON stays strict: removed volume config, deletion-disposition, claim,
and node-component fields are errors, not silently discarded data.

Controld performs a read-only startup check for any rows in the historical
`storage_volume_claims` and `storage_volume_bindings` tables. Any row refuses
startup, including deleted tombstones: a retained claim can still own physical
data even after its control state became terminal. Missing or empty historical
tables are accepted. The check does not inspect node disks and cannot prove
that a directory is empty or disposable.

## Existing Data

Before changing a deployment that used persistent volumes:

1. Stop new workload admissions under the archived release and use its
   supported lifecycle to quiesce workloads and inspect outstanding cleanup.
2. Preserve the old database and node state. Inventory claim identity,
   namespace and owner, selected node, reclaim policy, binding state, and exact
   physical directory. A source-code archive does not back up these data.
3. Use the archived release and its matching tools to export required data.
   Verify exported content and retain ownership and integrity metadata before
   accepting an artifact or another explicit destination.
4. Start the reduced release with new clean control and node state. Keep old
   state isolated and recoverable until its separate retention decision is
   authorized.

No migration in this retirement deletes rows, drops historical storage tables,
rewrites old payloads, or deletes node directories. There is no automatic
Retain-to-artifact conversion. Do not clear claims or remove directories just
to pass the startup check. Physical deletion, abandonment, and retention
expiry require an explicit inventory and separate authorization.
