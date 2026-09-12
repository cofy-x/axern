# Axrun Usage

Create and edit a TaskSetBuild project:

```bash
axrun task init --output-dir tasks/demo
axrun task build --file tasks/demo/taskset.yaml --output .axrun/tasksets/demo
axrun task inspect .axrun/tasksets/demo
```

`task init` writes explicit `250m` CPU and `512Mi` memory requests for its
starter task. Tune these per-episode requests to the actual agent workload;
do not rely on the control plane's conservative fallback.

`instruction.text` and `instruction.path` are mutually exclusive.
`workspace.expand` is explicitly `aggregate` or `per_match`; Axrun never
creates an implicit Cartesian product. Paths and globs are relative to the
build spec, sorted deterministically, and may not escape through links.
`per_match.task_id_prefix` is a short lowercase ASCII prefix; generated task
IDs retain a content hash and are bounded so task and episode paths remain
portable across supported filesystems.

Publish through Kova:

```bash
export KOVA_ENDPOINT=https://kova.example.com
export KOVA_TOKEN=...
axrun task publish .axrun/tasksets/demo \
  --target registry.example.com/axrun/tasksets/demo \
  --publisher kova
```

The token is process configuration, not TaskSet data. `--preheat` is explicit
and defaults off. Local development may use `--publisher local` for OCI only.

The Task owns sandbox source, workdir, verifier/oracle, outputs, resources,
timeouts, capabilities, and tags. The Rollout owns TaskSet reference,
agent/model, runner/placement, attempts, selection, concurrency, and outputs.
CPU and memory resources are applied per episode. `resources.disk` is rejected
until Axern exposes an enforceable ephemeral workspace disk contract; it is
never accepted and silently ignored.

Published TaskSets use `repository@sha256:...`; mutable tags and local build
paths are development inputs, not reproducible execution contracts. Run them
through the local CLI or HTTP rollout surface with either the local backend or
the Axern backend. Provider profiles remain local client configuration; Axern's
control plane does not own provider accounts, evaluation queues, or rollout
state.
