# Rollout Evidence Contract

This document defines the evidence contract for Axrun rollouts. Use it before changing artifact manifests, run-directory validation, or exports that consume execution refs.

The goal is simple: every completed write should remain diagnosable after failure, reproducible from native records, and exportable without scraping large inline logs.

## Scope

This contract applies to local CLI rollouts. The native run directory is the durable boundary; Axrun does not expose an HTTP rollout service.

It owns episode evidence, diagnosis read order, and implementation routing. It does not own external input adapters, Axern sandbox internals, or SDK wire contracts. Axrun does not expose a separate transient phase-event or product-error protocol: the CLI returns operation errors directly, while committed Run, Episode, result, trajectory, and manifest records provide durable diagnosis.

## Evidence Layout

The native run directory remains the source of truth:

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

`artifacts/manifest.json` should exist once an episode layout has entered the execution or collection path. If execution fails before artifact collection can run, the terminal error evidence should explain that instead of pretending that collection happened. Missing artifact content should be represented with `status = "missing"` or `status = "failed"` so consumers know collection was attempted.

Manifest entry shape:

```json
{
  "kind": "patch",
  "source": "/tmp/solution.patch",
  "path": "episodes/<episode_id>/artifacts/patches/solution.patch",
  "status": "present",
  "sha256": "optional",
  "error": "optional"
}
```

Recommended `kind` values should reuse `domain.ArtifactKind`: `agent_raw_log`, `agent_stdout`, `agent_stderr`, `downloaded_file`, `downloaded_directory`, `llm_telemetry`, `patch`, `runtime_image_build`, `trajectory_export`, `training_data_export`, and `verifier_breakdown`.

## Diagnosis Read Order

When diagnosing a rollout, read evidence in this order:

1. `run.json`: rollout identity, plan digest, lifecycle status, timestamps, and cached summary.
2. `plan.json`: frozen input, agent/model/provider behavior, runner, selected tasks, attempt budget, and episode order.
3. `episodes/*/episode.json`: attempt identity, lifecycle, failure class, timing, and sandbox binding.
4. `agent.json`: launcher kind, runtime image, mount target, `bin` directory, profile, exit reason, usage, raw-log refs, patch refs, and agent artifacts.
5. `verifier.json` and `reward.json`: verification result and outcome.
6. `trajectory.jsonl`: step-level agent and system events.
7. `artifacts/manifest.json`: declared evidence and capture status.
8. `exports/`: derived views only; never use exports as the source of truth.

Keep facts and inference separate. If a failure is not observable, add it to the authoritative lifecycle/result record or artifact manifest that owns the fact rather than introducing a parallel progress protocol or broad debug logging.

Resume never executes a started Episode again. A `running` or `verifying` Episode left by a process interruption is finalized as an infrastructure failure by changing only its lifecycle record; existing agent/verifier/reward files, trajectory, and artifacts remain byte-for-byte evidence. Only distinct, pre-planned `pending` Episode identities may execute. Terminal Episodes are never revived even when a sidecar or manifest is incomplete.

## Implementation Routing

| Change                          | Start With                                                                             |
| ------------------------------- | -------------------------------------------------------------------------------------- |
| Artifact manifest writes        | `internal/localstore` and `internal/rollout`                                           |
| Run validation rules            | `internal/schema` and `internal/application/validate`                                  |
| Export consumption of refs      | `internal/application/exportdata`                                                      |
| Agent-specific evidence         | `internal/agent/*`, `internal/proxy`, and `internal/rollout`                           |
| Axern sandbox evidence          | `internal/backend/axern` and public Axern SDK/API behavior                             |

For user-facing command examples, update [Usage](usage.md). For module-boundary rules, update [AGENTS.md](../AGENTS.md). For acceptance gates, update [Acceptance Matrix](acceptance.md).
