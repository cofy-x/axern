# Axrun Domain Model

Axrun records are the contract between compiled TaskSets, pluggable agent
execution, verification, trajectory capture, validation, and exports. External
task formats should be converted into `axrun/v1` `TaskSetBuild` specifications
before rollout planning.

## Core Records

### TaskInstance

`TaskInstance` is the compiled executable task contract. It carries the task id,
instruction, TaskSet source provenance, sandbox spec, initial state,
verifier, oracle, resources, timeouts, tags, capabilities, and metadata.

Task records must be self-contained after input capture. Paths and artifact
refs should be run-root-relative or input-root-relative at the boundary where
they are recorded.

### SandboxSpec

`SandboxSpec` describes the task environment. Its `runtime_source` is the
single source of truth for the sandbox rootfs:

- `template`: Axern template id.
- `image`: an immutable `sha256` Axern-consumable image ref. Axrun does not
  distinguish OCI and Nydus in this field.
- `dockerfile`: an internal non-TaskSet source. `TaskSetBuild` rejects it;
  publish a runtime image before compiling a TaskSet.

The TaskSet compiler resolves runtime intent into this field before publishing.

### AgentSpec

`AgentSpec` describes the selected agent, profile, approval policy, runtime,
prompt plan, tool policy, timeout, and artifact policy. Model identity remains
in `ModelSpec`.

Claude Code and Codex are managed adapters: they own their launch command and
enforce the recorded approval policy. `command` is the explicit deterministic
shell/custom-harness agent and has no managed provider or approval semantics.
Oracle/noop baselines and mounted managed bundles produce the same episode
evidence shape.

`AgentSpec.runtime.image` is an agent/tool bundle image. It is separate from
`TaskInstance.sandbox.runtime_source.image`, which defines the task
environment rootfs. With the Axern backend, this bundle is mounted read-only
into the task sandbox and the adapter-generated command runs through normal sandbox exec.
`runtime.mount_target` and `runtime.bin_dir` record the resolved mount
topology so runs, trajectories, and exports can be audited without inferring it
from agent names.

### RolloutRun

`RolloutRun` is the run envelope. It records TaskSet selection, agent/model
configuration, runner, execution options, schema version, timestamps, and refs
to captured inputs and derived outputs.

### RolloutPlan

`RolloutPlan` freezes the TaskSet descriptor digest, payload variant digests,
resolved task ids, task counts, attempts, selected split/shard metadata,
episode ids, and execution order. Resume reads the plan instead of resolving a
mutable tag or reopening build paths.

For a managed rollout, the durable plan also freezes the TaskSet source and
descriptor digests, ordered OCI/Nydus payload variants, immutable agent bundle
digest, resolved task IDs, Profile identity/name/version, and credential
version plus an internal-only credential reference. The public Rollout never
contains that internal reference.

### AgentProfile

`AgentProfile` is a namespace-scoped, versioned provider configuration. It
contains agent, provider, wire API, HTTPS base URL, maximum concurrency, labels,
Profile version, and credential version. Its credential is an owned encrypted,
immutable internal version: it is neither a generic user Secret nor a public
Profile field. Rotate changes the Profile and credential version atomically;
already accepted Rollouts retain their frozen version. Scheduling applies
`max_concurrency` to the frozen Profile ID/version group, so later Profile
updates cannot change admission for an existing READY or running Rollout.

### PreflightReport

`PreflightReport` is produced by a leased planning worker and saved by
controld. It records descriptor/source digests, task and episode counts,
Profile and credential versions, agent bundle digest, payload variants, typed
TaskSet/selection/Profile/provider/runtime/worker/budget checks, warnings, and
probe usage/cost. Provider HTTP execution belongs to the worker; controld only
schedules the lease, resolves the frozen snapshot, admits usage, and persists
the report.

### Episode

`Episode` is one attempt for one task under one rollout. It records task id,
attempt index, runner, status, exit reason, timing, usage, cost, verifier and
reward refs, artifact refs, and terminal completion metadata.

Managed episodes also store node-observed execution facts: the selected
workspace payload format and digest, cache result, image resolution/pull and
COW preparation durations, verifier materialization duration, allocation and
node identity, runtime class, and frozen agent bundle digest. These values are
transported through typed allocation and sandbox contracts rather than inferred
from logs or descriptor preference order.

An episode is complete only when it has a terminal status and the required
sidecars are present according to schema validation.

### VerifierResult

`VerifierResult` records verifier type, command or verifier identity, status,
exit code, stdout/stderr refs or summaries, timeout information, and metrics.
Task-specific verification details must already be represented as native
verifier metrics or artifact refs before execution.

### Reward

`Reward` normalizes verifier and task outcomes into score, pass/fail status,
failure classification, and metrics. Reward records should be stable enough
for evaluation and training exports.

### ArtifactRef

`ArtifactRef` points to captured files such as inputs, raw logs, patches,
verifier breakdowns, model proxy captures, downloaded directories, and export
sources. Refs are run-root-relative inside run records. Large bodies stay in
files and are not embedded in JSON records. Executed episodes publish
`artifact_manifest_path` as the stable index for present, missing, or failed
artifact capture.

Managed artifacts additionally have durable metadata: artifact ID, rollout and
episode generation, kind/name/media type, exact size, SHA-256, status, and
object key. The object key is internal. Public downloads use an artifact-bound
ticket and gatewayd stream; an object-store URL is not part of this model.

### ExportRecord

Exports are derived views over native run records:

- SFT export: task prompt, final agent output, reward summary, usage, and refs.
- Reward export: task, verifier, reward, status, usage, and refs.
- Trace export: trajectory rows and selected episode metadata.
- Preference export: chosen/rejected episode pairs grouped by task.

Exports must be reproducible from the run directory and should not become the
source of truth. Export refs include `artifact_manifest_path` so consumers can
find artifact status without scanning directories.
Export records use an agent summary rather than the full `AgentSpec`: command
argv, shell text, entrypoint, args, environment variables, local launcher paths,
inline prompt bodies, and session ids stay out of derived training and
evaluation views. The summary may include runtime type, bundle image, mount
target, `bin` directory, workdir, user, timeout, profile, prompt/session shape,
and artifact policy.

## Run Directory Contract

```text
.axrun/runs/<run_id>/
  run.json
  plan.json
  inputs/
  tasks/<task_id>/task.json
  episodes/<episode_id>/episode.json
  episodes/<episode_id>/trajectory.jsonl
  episodes/<episode_id>/agent.json
  episodes/<episode_id>/verifier.json
  episodes/<episode_id>/reward.json
  episodes/<episode_id>/artifacts/
  episodes/<episode_id>/artifacts/manifest.json
  exports/
```

`run.json` and `plan.json` define rollout intent. `tasks/*/task.json` records
the native task contract after input resolution. Episode sidecars record
execution, agent behavior, verification, reward, trajectory, artifact refs, and
the artifact manifest.

All refs written inside the run directory must be portable with the run root.
Absolute host paths should appear only as explicit external provenance, never
as required replay paths.

## TaskSet Boundary

The only rollout input is a TaskSet descriptor. A local bundle is accepted for
local development; production rollout references an immutable descriptor OCI
digest. `TaskSetBuild` owns instruction and workspace expansion and compiles
them to canonical `TaskInstance` records. Rollout owns only selection,
agent/model choice, scheduling, attempts, placement, and output location.

If execution needs a field, it must be expressed in a native task, verifier,
sandbox, oracle, metadata, or artifact ref field first.

## Execution Boundary

Episode execution receives compiled records plus selected backend configuration.
It may create sandboxes, mount a TaskSet workspace image, launch agents, run verifiers,
capture artifacts, and write sidecars. It must not re-open external task sources
to reinterpret task intent.

Each episode uses one task sandbox for the agent phase and verifier phase, so
the verifier observes the workspace after the agent has acted. Separate
attempts or tasks receive separate episode sandboxes.

The Axern backend sends ordered Nydus/OCI workspace variants to axnoded. It does
not upload the compiled workspace archive per episode. Local execution copies
from the local bundle to provide functional equivalence without the production
performance guarantee.

## Managed Lifecycle Boundary

`rollout plan` creates manual-start durable state. The planning worker may
produce tasks and a preflight report, after which controld writes `READY` and
keeps every episode work item `HELD`. `rollout start` atomically changes only
those held items to claimable work. `rollout run` uses auto-start and waits for
terminal state unless explicitly detached. Watch event sequence is monotonic
PostgreSQL state and is the resume cursor after a stream reconnect.

Profile credentials are resolved only for the matching active work lease.
READY and running Rollouts never re-read the current Profile. A credential
version can be collected only when no Profile, Rollout snapshot, or doctor job
references it.

## Schema Rules

- Schema validation is the gate before exports.
- Terminal episodes require `episode.json`, `agent.json`, `verifier.json`,
  `reward.json`, `trajectory.jsonl`, and `artifacts/manifest.json` according to
  status-specific rules.
- Artifact refs should include enough metadata for integrity checks when the
  artifact is part of validation or export.
- Raw LLM telemetry and command logs are referenced artifacts, not inline
  fields.
- New native fields should be added where they express execution semantics.
