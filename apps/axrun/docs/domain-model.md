# Axrun Domain Model

Axrun records are the contract between compiled TaskSets, pluggable agent execution, verification, trajectory capture, validation, and exports. External task formats should be converted into `axrun/v1` `TaskSetBuild` specifications before rollout planning.

## Core Records

### TaskInstance

`TaskInstance` is the compiled executable task contract. It carries the task id, instruction, TaskSet source provenance, sandbox spec, initial state, verifier, oracle, resources, timeouts, tags, capabilities, and metadata.

Task records must be self-contained after input capture. Paths and artifact refs should be run-root-relative or input-root-relative at the boundary where they are recorded.

### SandboxSpec

`SandboxSpec` describes the task environment. Its `runtime_source` is the single source of truth for the sandbox rootfs:

- `template`: Axern template id.
- `image`: an immutable `sha256` Axern-consumable image ref. Axrun does not distinguish OCI and Nydus in this field.
- `dockerfile`: an internal non-TaskSet source. `TaskSetBuild` rejects it; publish a runtime image before compiling a TaskSet.

The TaskSet compiler resolves runtime intent into this field before publishing.

### AgentSpec

`AgentSpec` describes the selected agent, profile, approval policy, runtime, prompt plan, tool policy, timeout, and artifact policy. Model identity remains in `ModelSpec`.

Claude Code and Codex are managed adapters: they own their launch command and enforce the recorded approval policy. `command` is the explicit deterministic shell/custom-harness agent and has no managed provider or approval semantics. Oracle/noop baselines and mounted managed bundles produce the same episode evidence shape.

`AgentSpec.runtime.image` is a caller-owned agent/tool image. It is separate from `TaskInstance.sandbox.runtime_source.image`, which defines the task environment rootfs. With the Axern backend, this image is mounted read-only into the task sandbox and the adapter-generated command runs through normal sandbox exec. `runtime.mount_target` and `runtime.bin_dir` record the resolved mount topology so runs, trajectories, and exports can be audited without inferring it from agent names.

Official Claude Code and Codex images are self-contained tool rootfs bundles with canonical mount targets under `/opt/axern/agents`. Their loader, system libraries, CA certificates, and agent runtime come from the bundle; the task rootfs continues to own its shell and project toolchain. Custom agent-image runtimes may select another single-name target under the same namespace.

### RolloutRun

`RolloutRun` is the mutable run envelope. It records the rollout identity, lifecycle status, timestamps, immutable plan path/digest, and a rebuildable summary. It does not copy TaskSet selection, Agent, Model, Sandbox, or task configuration from the plan.

### RolloutPlan

`RolloutPlan` freezes the TaskSet descriptor digest, captured input reference, resolved task ids, attempt budget, selected split/shard metadata, non-secret Agent/Model/provider behavior, runner, episode ids, and execution order. Resume reads the plan instead of resolving a mutable tag or reopening build paths. Credential values are resolved only when execution starts and are never plan fields.

### Episode

`Episode` is one immutable execution identity for one task attempt. It owns task/run references, `attempt_index`, lifecycle timestamps, failure class, timing, and its single Axern sandbox binding. It does not copy plan configuration, usage/cost, artifact indexes, or conventional sidecar paths.

An episode is complete only when it has a terminal status and completion marker and its conventional sidecars satisfy schema validation. `AgentResult` owns agent exit status, usage, cost, output refs, and agent-produced artifacts. `VerifierResult` owns verifier facts; `Reward` owns normalized scoring.

### VerifierResult

`VerifierResult` records verifier type, command or verifier identity, status, exit code, stdout/stderr refs or summaries, timeout information, and metrics. Task-specific verification details must already be represented as native verifier metrics or artifact refs before execution.

### Reward

`Reward` normalizes verifier and task outcomes into score, pass/fail status, failure classification, and metrics. Reward records should be stable enough for evaluation and training exports.

### ArtifactRef

`ArtifactRef` points to captured files such as inputs, raw logs, patches, verifier breakdowns, model proxy captures, downloaded directories, and export sources. Refs are run-root-relative inside their owning result or trajectory record. Large bodies stay in files and are not embedded in JSON records. The conventional `episodes/<episode_id>/artifacts/manifest.json` is the sole episode-wide artifact index.

### ExportRecord

Exports are derived views over native run records:

- SFT export: task prompt, final agent output, reward summary, usage, and refs.
- Reward export: task, verifier, reward, status, usage, and refs.
- Trace export: trajectory rows and selected episode metadata.
- Preference export: chosen/rejected episode pairs grouped by task.

Exports must be reproducible from the run directory and should not become the source of truth. Export refs include `artifact_manifest_path` so consumers can find artifact status without scanning directories. Export records use an agent summary rather than the full `AgentSpec`: command argv, shell text, entrypoint, args, environment variables, local launcher paths, inline prompt bodies, and session ids stay out of derived training and evaluation views. The summary may include runtime type, agent image, mount target, `bin` directory, workdir, user, timeout, profile, prompt/session shape, and artifact policy.

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

`plan.json` defines immutable rollout intent; `run.json` is its lifecycle envelope and cached aggregate. `tasks/*/task.json` records the native task contract after input resolution. Episode sidecars record execution, agent behavior, verification, reward, append-only trajectory, and the artifact manifest.

All refs written inside the run directory must be portable with the run root. Absolute host paths should appear only as explicit external provenance, never as required replay paths.

## TaskSet Boundary

The only rollout input is a TaskSet descriptor. A local bundle is accepted for local development; production rollout references an immutable descriptor OCI digest. `TaskSetBuild` owns instruction and workspace expansion and compiles them to canonical `TaskInstance` records. Rollout owns only selection, agent/model choice, scheduling, attempts, placement, and output location.

If execution needs a field, it must be expressed in a native task, verifier, sandbox, oracle, metadata, or artifact ref field first.

## Execution Boundary

Episode execution receives compiled records plus selected backend configuration. Axrun captures TaskSet inputs into immutable run-owned files before it creates sandboxes, uploads allocation-local inputs, launches agents, runs verifiers, captures outputs, and writes sidecars. Axern never receives TaskSet-specific storage objects. Episode execution must not re-open external task sources to reinterpret task intent.

Each episode uses one task sandbox for the agent phase and verifier phase, so the verifier observes the workspace after the agent has acted. Separate attempts or tasks receive separate episode sandboxes.

Axrun downloads and verifies the immutable TaskSet payload once into the local run directory. For each episode it uploads the selected workspace through Axern's public allocation-scoped archive API, then uploads verifier/oracle inputs only when their phase begins. Axern and axnoded do not interpret TaskSet objects and no node-local TaskSet COW or warm-upload-elision guarantee is currently made.

## Schema Rules

- Schema validation is the gate before exports.
- Terminal episodes require `episode.json`, `agent.json`, `verifier.json`, `reward.json`, `trajectory.jsonl`, and `artifacts/manifest.json` according to status-specific rules.
- Artifact refs should include enough metadata for integrity checks when the artifact is part of validation or export.
- Raw LLM telemetry and command logs are referenced artifacts, not inline fields.
- New native fields should be added where they express execution semantics.
