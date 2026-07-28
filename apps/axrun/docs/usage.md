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

Local bundles support compiler and publisher development. Managed Rollouts use
`repository@sha256:...`; mutable tags and local build paths are never durable
execution contracts.

## Managed rollouts

Create a namespace-scoped Profile without putting plaintext credentials in an
argument, YAML, or generic Secret API:

```bash
axrun profile create production-codex --agent codex --provider openai \
  --wire-api responses --base-url https://api.openai.com/v1 \
  --max-concurrency 16 --token-stdin
axrun profile doctor production-codex --model <model>
axrun profile update production-codex --max-concurrency 32
axrun profile rotate production-codex --token-stdin
```

`--token-stdin` and `--token-env NAME` are mutually exclusive. Profile
responses contain Profile and credential version numbers, never a credential
ID or plaintext. Update and rotate accept `--expected-version` and
`--idempotency-key`; the CLI obtains the current version and generates a key
when omitted.

Submit the rollout contract to the durable control plane:

```bash
axrun rollout plan --file rollout.yaml
axrun rollout start <ready-rollout-id>
axrun rollout run --file rollout.yaml
axrun rollout run --file rollout.yaml --detach
axrun rollout get <rollout-id>
axrun rollout list --status running --agent codex --model <model>
axrun rollout watch <rollout-id> --until terminal
axrun rollout inspect <rollout-id>
axrun rollout cancel <rollout-id>
axrun rollout retry <rollout-id>
axrun rollout artifact list <rollout-id>
axrun rollout artifact download <artifact-id> --output evidence.json
axrun rollout artifact download-all <rollout-id> --output-dir evidence
axrun rollout delete <rollout-id>
axrun rollout compare <rollout-id-a> <rollout-id-b>
```

`plan` waits for worker-side TaskSet resolution, selection, Profile snapshot,
provider probe, budget check, and immutable agent bundle validation, then
returns a manual-start `READY` rollout whose episode work remains `HELD`.
`start` consumes that frozen plan without re-planning. `run` waits for terminal
state by default; `--detach` returns only after durable acceptance. Ctrl-C
detaches with exit code 130 and prints the rollout ID; it never cancels the
remote rollout. Watch resumes from its last event sequence after retryable
disconnects and does not retry permanent authentication or argument errors.
For streaming `plan`, `start`, `run`, and `watch` commands, `--format json`
is newline-delimited JSON: every event is one compact record and the final
record is the durable Rollout. Non-streaming commands return one JSON document.

Terminal exit codes are stable: `0` passed, `10` task/verifier failure, `11`
infrastructure failure, `12` budget/metering failure, `13` cancelled, `14`
planning/preflight rejection, `1` client/protocol error, and `2` usage error.
Retrying creates a new execution generation only for eligible infrastructure
failures; completed evidence is not silently replaced.
`compare` aligns immutable task digests and reports reward/pass/cost evidence
without downloading local run directories.

Downloads use a short-lived artifact-bound ticket and gatewayd's mTLS gRPC
stream. Axrun resumes an adjacent `.part`, refreshes expired tickets, verifies
exact size and SHA-256, and atomically publishes the destination. It never
receives an object-store URL or credential and does not overwrite without
`--force`.

An operator runs workers against controld's private mTLS endpoint:

```bash
export AXRUN_WORKER_TOKEN=...
axrun --config /etc/axern/config.json worker \
  --context cluster-control \
  --execution-context cluster-execution \
  --concurrency 16 --output-dir /var/lib/axrun
```

The bootstrap token establishes a short-lived worker session. Every claimed
work item then uses a separate renewable lease token. The selected root context
must address controld's private worker API; `--execution-context` must address
gatewayd's public execution API. Keeping these authorities separate prevents a
worker credential from turning into a general gateway credential and keeps
sandbox data-plane routing out of controld. The Helm chart generates both
contexts when `rolloutWorker.enabled=true`; durable evidence additionally
requires either the bundled MinIO or an external object store.
