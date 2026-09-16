# Axrun Architecture

```text
TaskSetBuild
  -> deterministic compiler
  -> local bundle (descriptor + normalized payload)
  -> Kova HTTP build (OCI + Nydus)
  -> immutable descriptor artifact
  -> frozen RolloutPlan
  -> Axrun OCI payload capture
  -> allocation-local Archive upload
  -> runsc sandbox
```

## Ownership

- Axrun owns TaskSet semantics, compilation, descriptor publication, selection, rollout planning, evidence, resume, and export.
- Kova owns distributed BuildKit execution, registry push, Nydus conversion, typed build results, and optional Dragonfly preheat.
- Axrun owns payload selection, immutable OCI download, safe extraction, and local run-input capture.
- Axern owns sandbox lifecycle and allocation-local file/archive transfer without understanding TaskSet, verifier, or oracle semantics.

The caller-supplied agent image remains an independent read-only `ImageMount`. The task rootfs and captured input directory are separate execution inputs, not durable Axern product objects.

## Platform boundary

Axrun is an SDK consumer and reference workflow, not an Axern control-plane subsystem. It owns local rollout plans, episode state, trajectories, verifier results, rewards, and exports. Axern owns only the secure, rebuildable sandbox resources used to execute those episodes. Provider credentials and evaluation or training orchestration remain with the caller or a separate upper-layer system.

## Determinism and immutability

The compiler rejects unknown fields, empty globs, escaping paths, symlinks, hardlinks/devices, mutable runtime images, episode-local Dockerfile builds, and duplicate task IDs. Payload entries are UTF-8-byte ordered; tar uid/gid/time, xattrs, directories, and non-executable file modes are normalized while the executable bit is retained. `source_digest` describes logical payload content and does not change when a registry or backend produces a different manifest digest.

The published descriptor is an image manifest with exactly one logical `application/vnd.axrun.taskset.v1+json` descriptor. Axrun accepts its native OCI envelope and the Docker schema 2 envelope produced by registries that normalize OCI artifacts. A normalized Docker layer must be a bounded tar containing only the regular file `descriptor.json`; indexes, manifest lists, additional files, and all other layer types are rejected before the strict descriptor contract check.

Planning accepts a local bundle or an immutable OCI descriptor reference. Before execution, Axrun pulls the required OCI payload, rejects unsafe archive entries, resolves the selected task paths, and copies the workspace, verifier, and oracle inputs into the local run directory. Resume reads only that frozen local state. Axern receives generic archive uploads and returns allocation identity and runtime state; it does not persist TaskSet-specific preparation facts.

The local run directory is the current authority. `plan.json` is published once and its semantic digest is bound by `run.json`; loaders verify that binding before using the plan. Mutable authoritative JSON uses same-directory temporary files, file sync, atomic rename, and directory sync. Trajectory files have one run-lock-protected appender, and an Episode artifact manifest is published once after its referenced files are complete. A kernel-backed per-run file lock covers create or resume execution and is released automatically when the owning process exits; Axrun does not infer stale ownership from timestamps.

Resume never reuses an Episode identity. It finalizes an interrupted `running` or `verifying` Episode as an infrastructure failure without manufacturing an execution duration or changing existing sidecars, trajectory, or artifacts. Only a distinct pre-planned pending attempt may execute, and concurrency always comes from the frozen plan.

## Provider credentials

Agent Profile is owner-local execution configuration, not a durable Axrun or Axern object. A profile containing a plaintext token must be a regular owner-only file and cannot be loaded through a symbolic link. The immutable plan records only normalized provider behavior and a non-secret fingerprint; rotating a token does not alter that fingerprint, while changing the endpoint, provider, wire API, environment, or provider configuration does. Credential-like runtime environment keys are rejected from rollout inputs so tokens are resolved and injected only by the selected agent adapter during execution.

## Security phases

Axrun captures verifier and oracle inputs separately from the initial workspace. It uploads the workspace before the agent phase and uploads verifier or oracle files only when their owning phase needs them. TaskSet build and capture reject escaping paths and linked inputs; Axern's lease-authenticated generic file/archive APIs enforce the Allocation boundary.
