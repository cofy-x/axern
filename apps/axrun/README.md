# axrun

`axrun` compiles immutable TaskSets and executes reproducible agent rollouts.
Task semantics live in `TaskSetBuild`; rollout specs select an immutable TaskSet,
agent/model, attempts, placement, and output location.

## Commands

```text
axrun task init --output-dir <dir>
axrun task build --file <taskset.yaml> --output <bundle-dir>
axrun task publish <bundle-dir> --target <registry/repo> [--publisher kova|local]
axrun task inspect <local-path-or-oci-ref>
axrun rollout plan --file rollout.yaml
axrun rollout run --file rollout.yaml
axrun rollout run --resume <run-dir>
axrun validate <run-dir>
axrun export sft|reward|trace|preference <run-dir>
axrun serve
```

`task build` is deterministic and offline. `task publish` is the only TaskSet
operation that writes to a registry. Kova is the production default and emits
Nydus plus OCI variants; `local` pushes an OCI variant for development.

## Rollout

```yaml
api_version: axrun/v1
kind: Rollout
metadata:
  name: example
spec:
  task_set:
    ref: registry.example.com/axrun/tasksets/demo@sha256:...
  agent:
    name: codex
    runtime:
      kind: agent_image
      image: registry.example.com/agents/codex@sha256:...
    profile: production
    approval_policy: never
  model: openai/gpt-5
  execution:
    runner: axern
    namespace: default
    runtime_class: runsc
    concurrency: 32
    attempts: 4
  selection:
    task_ids: []
    limit: 0
    shard_index: 0
    shard_count: 0
  output_dir: .axrun/runs
```

Remote execution requires immutable task and image references. Planning freezes
the resolved task selection, payload variants, agent bundle, and episode order
into the local run directory. Execution then uses the selected local or Axern
backend, and resume reads the frozen plan instead of re-resolving mutable input.
Provider profiles remain local client configuration; controld does not own
provider credentials, rollout queues, or evaluation results.

See [usage](./docs/usage.md), [architecture](./docs/architecture.md), and
[acceptance](./docs/acceptance.md).
