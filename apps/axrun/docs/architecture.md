# Axrun Architecture

```text
TaskSetBuild
  -> deterministic compiler
  -> local bundle (descriptor + normalized payload)
  -> Kova HTTP build (OCI + Nydus)
  -> immutable descriptor artifact
  -> frozen RolloutPlan
  -> Axern WorkspaceImageSource
  -> node-local COW workspace
  -> phase-gated verifier/oracle materialization
```

## Ownership

- Axrun owns TaskSet semantics, compilation, descriptor publication, selection,
  rollout planning, evidence, resume, and export.
- Kova owns distributed BuildKit execution, registry push, Nydus conversion,
  typed build results, and optional Dragonfly preheat.
- Axern owns payload variant selection, imagefs/Nydus resolution, active cache
  references, allocation-local overlay COW, sandbox lifecycle, and protected
  asset materialization.

The mounted agent image remains an independent read-only `ImageMount`. A task
rootfs, agent bundle, and TaskSet workspace are three distinct resources.

## Platform boundary

Axrun is an SDK consumer and reference workflow, not an Axern control-plane
subsystem. It owns local rollout plans, episode state, trajectories, verifier
results, rewards, and exports. Axern owns only the secure, rebuildable sandbox
resources used to execute those episodes. Provider credentials and evaluation
or training orchestration remain with the caller or a separate upper-layer
system.

## Determinism and immutability

The compiler rejects unknown fields, empty globs, escaping paths, symlinks,
hardlinks/devices, mutable runtime images, episode-local Dockerfile builds, and
duplicate task IDs. Payload entries are UTF-8-byte ordered; tar uid/gid/time,
xattrs, directories, and non-executable file modes are normalized while the
executable bit is retained.
`source_digest` describes logical payload content and does not change when a
registry or backend produces a different manifest digest.

The published descriptor is an image manifest with exactly one logical
`application/vnd.axrun.taskset.v1+json` descriptor. Axrun accepts its native OCI
envelope and the Docker schema 2 envelope produced by registries that normalize
OCI artifacts. A normalized Docker layer must be a bounded tar containing only
the regular file `descriptor.json`; indexes, manifest lists, additional files,
and all other layer types are rejected before the strict descriptor contract check.

Planning accepts a local bundle or an immutable OCI descriptor reference. Run
inputs store the descriptor and digest references, not the original source
tree. Remote episodes carry ordered Nydus/OCI variants and a task workspace
subpath. No client workspace archive is uploaded for this path.

The node returns typed `WorkspacePreparationFacts` from allocation creation.
Controld stores them on the allocation and exposes them through the service
replica read model; the SDK carries them into sandbox state. Verifier
materialization returns its node-observed duration directly. Axrun combines
those values with allocation identity, runtime class, and the frozen agent
bundle digest in terminal episode execution facts. Logs are observability, not
the source of truth for this contract.

## Security phases

The initial workspace exposes only `tasks/<id>/workspace`. The node retains the
payload image reference but does not expose verifier or oracle prefixes.
After the agent phase, Axrun calls the lease-authenticated materialization API
for verifier assets. Oracle materialization requires the oracle harness and the
task's explicit `oracle_assets` capability. Source and target paths are checked
against the allocation's task prefix and COW workspace; links and overwrites
are rejected.
