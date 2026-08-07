---
title: Axrun Managed Rollouts
description: Compile immutable tasks and run reproducible, verified agent rollouts on Axern.
---

Axrun is Axern's native agent harness, task compiler, rollout client, verifier,
and trajectory exporter. It stays above the sandbox platform and consumes only
public Axern APIs.

![Axrun rollout command overview](/terminal/axrun.gif)

## Before you start

Managed Rollouts require:

- a running Axern control plane and an active CLI context with permission to
  create workloads in the target namespace;
- the `axrun` binary from the same release as the Axern gateway;
- a TaskSet repository published as an immutable
  `repository@sha256:...` reference, with the rollout worker able to pull it;
- a versioned provider profile and the namespace policy needed by the rollout.

Local TaskSet bundles are useful for compiler development, but they are not a
managed rollout artifact and cannot replace repository publishing.

```mermaid
flowchart LR
    Build["TaskSetBuild"] --> Compile["Deterministic compile"]
    Compile --> Artifact["Immutable TaskSet digest"]
    Artifact --> Plan["Plan + provider probe"]
    Profile["Versioned Profile"] --> Plan
    Plan --> Ready["READY frozen rollout"]
    Ready --> Execute["Axern sandbox episodes"]
    Execute --> Verify["Verifier + reward"]
    Verify --> Evidence["Artifacts, trajectory, usage"]
```

## Build and inspect a TaskSet

```bash
axrun task init --output-dir tasks/demo
axrun task build --file tasks/demo/taskset.yaml --output .axrun/tasksets/demo
axrun task inspect .axrun/tasksets/demo
```

Local bundles support compiler development. Managed Rollouts require an
immutable `repository@sha256:...` reference published through the configured
artifact path.

## Configure a managed profile

Pass credentials on stdin so they do not enter argv, YAML, or generic Secret
APIs. Read them from an existing environment variable or a protected input
source instead of writing the value literally in command history:

```bash
axrun profile create production \
  --agent codex \
  --provider openai \
  --wire-api responses \
  --base-url https://api.openai.com/v1 \
  --max-concurrency 16 \
  --token-stdin

axrun profile doctor production --model <model>
```

## Plan, start, and inspect

```bash
axrun rollout plan --file rollout.yaml
axrun rollout start <ready-rollout-id>
axrun rollout watch <rollout-id> --until terminal
axrun rollout artifact list <rollout-id>
axrun rollout artifact download-all <rollout-id> --output-dir evidence
```

Planning freezes task selection, payloads, agent image, Profile version, hidden
credential version, and model contract. Managed artifact bytes return through
gatewayd's mTLS streaming API; Axrun never receives object-store credentials.

Read the [complete Axrun usage contract](https://github.com/cofy-x/axern/blob/main/apps/axrun/docs/usage.md)
for exit codes, streaming JSON, cancellation, retry, and worker behavior.
