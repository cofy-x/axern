# Rollout Evidence Contract

This document defines the evidence contract for Axrun rollouts. Use it
before changing HTTP SSE events, rollout phase reporting, error shape, artifact
manifests, run-directory validation, or exports that consume execution refs.

The goal is simple: every rollout should be observable while it runs,
diagnosable after it fails, reproducible from native records, and exportable
without scraping large inline logs.

## Scope

This contract applies to local CLI/HTTP rollouts and durable managed rollouts.
Local entrypoints share the rollout application service. Managed lifecycle,
diagnosis, event sequence, preflight, and artifact inventory are durable typed
controld records; the CLI does not infer them from text.

It owns lifecycle phase names, SSE shape, product error categories, episode
evidence, diagnosis read order, and implementation routing. It does not own
external input adapters, Axern sandbox internals, or SDK wire contracts.

## Phase Contract

Rollout phases are progress signals, not complete logs. A phase may be emitted
once per run or once per episode depending on where the work happens.

| Phase | Meaning |
| --- | --- |
| `planning` | Request normalized, tasks selected, attempts/shards resolved, plan prepared, and a leased worker records real-provider preflight usage/cost and typed checks before episode work is claimable. |
| `preparing_inputs` | Inputs, task records, workspaces, verifier assets, or runtime image sources are captured into the run directory. |
| `sandbox_creating` | Backend preflight or sandbox creation is in progress for an executing episode. |
| `agent_running` | Agent launcher is running and trajectory/raw-log evidence may be produced. |
| `verifying` | Verifier is running or verifier result sidecars are being produced. |
| `collecting` | Agent, verifier, reward, trajectory, patches, and declared artifacts are being finalized. |
| `validating` | Native run-directory schema and cross-file contracts are being checked. |
| `exporting` | Optional derived export views are being written. |

`planning` and `preparing_inputs` happen before episode execution.
`sandbox_creating`, `agent_running`, `verifying`, and `collecting` are
episode-scoped when execution is enabled. `verifying` may be absent when a task
has no verifier, and `exporting` is absent unless export work is requested. A
terminal event must be emitted exactly once for an accepted HTTP request.

For managed rollouts, `READY` is a non-terminal manual-start boundary. Its
episode work is `HELD`, so it cannot be claimed before `StartRollout`. Durable
events use strictly increasing PostgreSQL `sequence`; watchers acknowledge the
highest rendered sequence, discard duplicates, and reconnect from
`after_sequence`. Ctrl-C detaches and does not create a cancel event.

## Managed diagnosis and exit status

`DiagnoseRollout` classifies durable facts as planning rejected, queue waiting,
worker unavailable, Profile/provider failure, capacity wait, task/verifier
failure, infrastructure failure, budget exhaustion, cancel pending, or
incomplete evidence, with one stable recommended action. CLI terminal status
is `0` for all passed, `10` for task/verifier failure, `11` for infrastructure,
`12` for budget/metering, `13` for cancellation, and `14` for planning or
preflight rejection. Client/protocol and usage errors remain `1` and `2`.
The Rollout row carries the durable terminal failure class so planning failures
remain typed even when no Episode exists; a typed failed preflight check takes
precedence for diagnosis and exit code `14`.

## SSE Events

HTTP SSE should expose status and lightweight evidence. It should not stream
complete agent stdout, verifier logs, raw LLM request bodies, or large artifact
contents.

Event names:

| Event | Required Fields | Meaning |
| --- | --- | --- |
| `run.accepted` | `run_id`, `status` | Request passed pre-SSE validation and has a run identity. |
| `run.phase` | `run_id`, `phase`, `status` | Run or episode reached a lifecycle phase. |
| `run.completed` | `run_id`, `status`, `summary` | Rollout reached a terminal successful application state. |
| `run.failed` | `run_id`, `error` | Rollout failed after acceptance. |

Common optional fields are `episode_id`, `task_id`, `attempt_index`,
`duration_ms`, `evidence`, and `error`.

`evidence` is a small JSON object for stable refs such as `run_json`,
`episode_json`, `agent_json`, `verifier_json`, `reward_json`, `trajectory`, or
`artifact_manifest`. Paths should be run-root-relative when persisted in run
records and may be server-local in transient SSE responses.

## Error Contract

Errors should be small, stable, and product-shaped. The message may change; the
code should not churn without updating this document and tests.

`error.code` is for execution diagnosis and API behavior. It is related to, but
separate from, `episode.failure_class`, which is the training/evaluation
outcome class. For example, `AGENT_TIMEOUT` may map to
`FailureClassTimeout`, but neither field replaces the other.

| Code | Phase | Retriable | Meaning |
| --- | --- | --- | --- |
| `INPUT_INVALID` | `planning` | no | Request, CLI flags, or task selection are invalid. |
| `INPUT_RESOLUTION_FAILED` | `planning` | maybe | Native manifest or task source cannot be resolved. |
| `TASK_RUNTIME_SOURCE_MISSING` | `preparing_inputs` | no | No explicit task runtime source or configured backend default exists. |
| `RUNTIME_IMAGE_PREPARE_FAILED` | `preparing_inputs` | maybe | Dockerfile task runtime image build/import failed. |
| `SANDBOX_CREATE_FAILED` | `sandbox_creating` | maybe | Backend could not create or prepare the task sandbox. |
| `AGENT_FAILED` | `agent_running` | no | Agent command or harness failed with a task-level failure. |
| `AGENT_TIMEOUT` | `agent_running` | no | Agent exceeded its configured timeout. |
| `VERIFIER_FAILED` | `verifying` | no | Verifier ran and produced a failing result. |
| `VERIFIER_TIMEOUT` | `verifying` | no | Verifier exceeded its configured timeout. |
| `ARTIFACT_CAPTURE_FAILED` | `collecting` | maybe | Declared artifact, patch, raw log, or download capture failed. |
| `VALIDATION_FAILED` | `validating` | no | Native run-directory validation failed. |
| `EXPORT_NOT_READY` | `exporting` | no | Export was requested before required evidence was complete. |
| `INFRA_FAILURE` | any | maybe | Unexpected infrastructure or backend failure outside task semantics. |

Error payload shape:

```json
{
  "code": "AGENT_TIMEOUT",
  "message": "agent exceeded timeout",
  "phase": "agent_running",
  "component": "agent",
  "retriable": false,
  "evidence": {"agent_json": "episodes/ep-1/agent.json"}
}
```

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

`artifacts/manifest.json` should exist once an episode layout has entered the
execution or collection path. If execution fails before artifact collection can
run, the terminal error evidence should explain that instead of pretending that
collection happened. Missing artifact content should be represented with
`status = "missing"` or `status = "failed"` so consumers know collection was
attempted.

Manifest entry shape:

```json
{
  "kind": "patch",
  "source": "/tmp/solution.patch",
  "path": "artifacts/patches/solution.patch",
  "status": "present",
  "sha256": "optional",
  "error": "optional"
}
```

Recommended `kind` values should reuse `domain.ArtifactKind`: `agent_raw_log`,
`agent_stdout`, `agent_stderr`, `downloaded_file`, `downloaded_directory`, `llm_telemetry`,
`patch`, `runtime_image_build`, `trajectory_export`, `training_data_export`,
and `verifier_breakdown`.

Managed episode acceptance requires the six evidence families produced by the
native run: episode, agent, verifier, reward, trajectory, and artifact manifest
or its committed durable inventory. Every uploaded artifact is `PENDING` until
the worker upload is verified and committed. Missing or failed objects remain
explicit evidence states; they are never represented by a successful rollout
plus an absent file.

## Managed artifact download

`PrepareArtifactDownload` returns public metadata and a short-lived HMAC ticket
bound to artifact ID, execution generation, digest, size, expiry, and gateway
audience. The private resolver returns a presigned request only to gatewayd.
Gatewayd enforces concurrency, maximum size, response-header timeout, exact
range, bounded chunks, backpressure, and full-stream size/digest checks. Axrun
resumes a neighboring `.part`, refreshes an expired ticket, verifies exact
size/SHA-256, and atomically publishes the destination. Tickets, signed query
strings, authorization headers, and internal URLs are not evidence and must
not appear in logs, traces, metrics labels, JSON output, or errors.

## Diagnosis Read Order

When diagnosing a rollout, read evidence in this order:

1. `run.json`: status, input, agent, model, backend, summary, and output.
2. `plan.json`: selected tasks, attempts, shards, and resume plan.
3. `episodes/*/episode.json`: terminal episode state and sidecar refs.
4. `agent.json`: launcher kind, runtime image, mount target, `bin` directory,
   profile, exit reason, usage, raw-log refs, patch refs, and agent artifacts.
5. `verifier.json` and `reward.json`: verification result and outcome.
6. `trajectory.jsonl`: step-level agent and system events.
7. `artifacts/manifest.json`: declared evidence and capture status.
8. `exports/`: derived views only; never use exports as the source of truth.

Keep facts and inference separate. If a failure is not observable, prefer
adding one structured phase or manifest field over broad debug logging.

Resume treats completed terminal episodes as immutable evidence when their
sidecars and artifact manifest are present. Interrupted terminal episodes with
missing completion evidence, missing sidecars, or missing
`artifacts/manifest.json` are resumable; pre-planned later attempts are the
preferred retry path for preserving failed attempt evidence.

## Implementation Routing

| Change | Start With |
| --- | --- |
| SSE event names or payloads | `internal/application/server` and this document |
| Phase production during rollout | `internal/application/rollout` and `internal/rollout` |
| Error codes or mapping | `internal/contract`, `internal/application/rollout`, and `internal/application/server` |
| Artifact manifest writes | `internal/localstore` and `internal/rollout` |
| Run validation rules | `internal/schema` and `internal/application/validate` |
| Export consumption of refs | `internal/application/exportdata` |
| Agent-specific evidence | `internal/agent/*`, `internal/proxy`, and `internal/rollout` |
| Axern sandbox evidence | `internal/backend/axern` and public Axern SDK/API behavior |
| Managed watch/outcome/download | `internal/application/managedrollout` |
| Durable diagnosis/inventory/ticket issue | `control/controld` rollout API/store |
| Artifact byte streaming | `gateway/gatewayd/internal/{api,application,adapters}/artifact` |

For user-facing command examples, update [Usage](usage.md). For module-boundary
rules, update [AGENTS.md](../AGENTS.md). For acceptance gates, update
[Acceptance Matrix](acceptance.md).
