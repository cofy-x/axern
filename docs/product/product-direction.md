# Axern Long-Term Direction

This document defines Axern's durable product direction. It is not a release
plan, milestone tracker, or feature backlog. Use it to judge whether a proposed
design moves the platform toward the intended product.

## North Star

Axern is a programmable execution platform for AI agents, AI coding, and
sandbox workloads. A user should be able to create an isolated environment,
run or serve code, connect to it securely, observe its lifecycle, retain the
right artifacts, and clean it up through consistent APIs and SDKs.

The platform remains general enough for non-agent sandbox workloads, while
product decisions prioritize long-running and task-oriented agent execution.

## Durable Product Principles

- **Control-plane first:** durable intent, identity, placement, policy, and
  lifecycle state belong in product APIs rather than client-side orchestration.
- **Sandbox as the primitive:** agent harnesses, coding workspaces, Functions,
  and services compose the same environment, execution, storage, network, and
  observability capabilities instead of introducing special runtime shortcuts.
- **Secure remote access:** files, processes, terminals, HTTP services, and
  tunnels use explicit, revocable, task-scoped authorization.
- **Observable by default:** lifecycle state, logs, metrics, traces, inventory,
  usage, trajectories, and artifacts have clear owners and stable identities.
- **Runtime choice behind one model:** runc, runsc, image formats, and node
  implementations may vary without fragmenting the user-facing resource model.
- **Local-to-production continuity:** daily development environments exercise
  the same contracts used by deployed systems, with deeper Linux or cluster
  validation reserved for behavior that needs it.
- **Composable SDKs:** CLIs and agent products build on public APIs and SDKs;
  they do not become alternate control planes.

## Long-Term Capability Areas

- Programmable sandbox lifecycle and process, file, terminal, and proxy APIs.
- Persistent agent coding workspaces composed from Service, Environment,
  Volume, and Tunnel primitives: workspace owns project data, profile owns
  local agent credentials, Service owns compute, and Tunnel owns one session.
- Agent-oriented task execution, verification, trajectory capture, and artifact
  retention through Axrun and related product layers.
- Services and Functions with readiness, rollout, warm capacity,
  scale-to-zero, invocation history, and explicit handler contracts.
- Task-scoped secrets, persistent and ephemeral storage, controlled egress,
  service ingress, and reverse tunnels.
- Runtime templates for coding, browser, research, CI, and data workloads
  without marketplace or template sprawl.
- Usage and billing primitives based on time, resources, runtime class,
  storage, network exposure, and retained state.
- A future workload family for batch, training, RL, or experiments when their
  queueing, retry, checkpoint, concurrency, and budget semantics are defined;
  do not overload Service replicas to approximate them.

## Product Boundaries

- Axern is not a general Kubernetes replacement.
- Node-local `axctl` is operator and debugging tooling, not the product CLI.
- Agent and coding products must not depend on one provider or one coding-agent
  implementation.
- The core platform does not absorb application-specific protocol behavior
  that can be implemented through public sandbox and network primitives.
- Compatibility with an early internal model is not a goal when a coherent
  redesign can update all in-repository consumers together.

## Applying This Direction

For a proposal, ask:

1. Which durable user capability does it add or simplify?
2. Which component owns its state and lifecycle?
3. Does it reuse the sandbox and public API model or create a parallel path?
4. What security, observability, cleanup, and usage contracts does it need?
5. Which local and production-like environments can validate it?

Keep concrete engineering rules in the root [Agent Contract](../../AGENTS.md)
and local contracts. Keep current system behavior in architecture documents,
commands in runbooks or owning READMEs, and planned work in the issue tracker.
