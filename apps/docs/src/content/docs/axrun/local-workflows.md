---
title: TaskSets and Local Workflows
description: Compile immutable TaskSets, validate run directories, and export trajectories for training and evaluation.
---

[Managed rollouts](/axrun/) run against the durable control plane. The local
workflow around them — compiling TaskSets, inspecting run directories, and
exporting derived views — works the same way for local development and
production evidence.

## Compile a TaskSet

A TaskSetBuild spec is strict `axrun/v1` YAML. The compiler resolves
instructions and workspace expansion deterministically into immutable
`TaskInstance` records; it never creates an implicit Cartesian product:

```bash
axrun task init --output-dir tasks/demo
axrun task build --file tasks/demo/taskset.yaml --output .axrun/tasksets/demo
axrun task inspect .axrun/tasksets/demo
```

`task init` writes explicit `250m` CPU and `512Mi` memory requests for its
starter task. Tune these per-episode requests to the actual agent workload
instead of relying on the control-plane fallback. `resources.disk` is rejected
until Axern exposes an enforceable ephemeral disk contract.

Local bundles support compiler development. Managed rollouts require an
immutable `repository@sha256:...` reference published through Kova:

```bash
export KOVA_ENDPOINT=https://kova.example.com
export KOVA_TOKEN=...
axrun task publish .axrun/tasksets/demo \
  --target registry.example.com/axrun/tasksets/demo \
  --publisher kova
```

## The run directory

Every rollout writes a portable run directory that is the source of truth for
validation and exports:

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
  episodes/<episode_id>/artifacts/manifest.json
  exports/
```

`run.json` and `plan.json` freeze rollout intent; episode sidecars record
execution, agent behavior, verification, reward, trajectory, and artifact
references. All references are relative to the run root so the directory can
move as one unit.

## Validate and export

Schema validation is the gate before exports:

```bash
axrun validate .axrun/runs/<run_id>

axrun export sft .axrun/runs/<run_id> --output-file sft.jsonl
axrun export reward .axrun/runs/<run_id>
axrun export trace .axrun/runs/<run_id>
axrun export preference .axrun/runs/<run_id>
```

Exports are derived views — SFT prompts and outputs, reward rows, trajectory
traces, and chosen/rejected preference pairs — reproducible from the run
directory. Raw LLM telemetry and command logs stay referenced artifacts, never
inline fields.

Terminal exit codes are stable for automation: `0` passed, `10` task or
verifier failure, `11` infrastructure failure, `12` budget or metering
failure, `13` cancelled, `14` planning rejection, `1` client error, `2` usage
error.

The [usage contract](https://github.com/cofy-x/axern/blob/main/apps/axrun/docs/usage.md)
and [domain model](https://github.com/cofy-x/axern/blob/main/apps/axrun/docs/domain-model.md)
are the authoritative references.
