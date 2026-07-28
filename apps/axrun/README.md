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
axrun profile create|get|list|update|rotate|doctor|delete
axrun rollout plan --file rollout.yaml
axrun rollout start <ready-rollout-id>
axrun rollout run --file rollout.yaml [--detach]
axrun rollout watch|inspect|get|list|cancel|retry|delete|compare
axrun rollout artifact list|download|download-all
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

Remote execution requires a descriptor digest. Planning resolves the small
descriptor and freezes its digest, logical source digest, selected task IDs,
payload variants, agent bundle digest, Profile version, and hidden credential
version. Managed `rollout plan` performs the real provider probe from an Axrun
worker and creates a directly startable `READY` rollout; controld only owns
durable state, scheduling, snapshots, metering admission, and saved results.
Every managed provider probe and episode is durably metered even without a
configured budget. Managed artifact bytes always flow through gatewayd's mTLS
gRPC data plane.
Resume reads the frozen plan.

See [usage](./docs/usage.md), [architecture](./docs/architecture.md), and
[acceptance](./docs/acceptance.md).
