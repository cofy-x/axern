# Axern Long-Term Direction

This document defines Axern's durable product direction. It is not a release plan, milestone tracker, or feature backlog. Use it to judge whether a proposed design moves the platform toward the intended product.

## North Star

Axern is an open-source environment execution platform for agent evaluation, training, and data synthesis. A unified SDK provides secure, rebuildable, high-concurrency sandbox infrastructure for creating environments, executing agents and tools, exporting results, and cleaning up.

The platform remains general enough for non-agent sandbox workloads, while product decisions prioritize repeatable agent workloads and large-scale experiment execution.

## Durable Product Principles

- **Control-plane first:** durable intent, identity, placement, policy, and lifecycle state belong in product APIs rather than client-side orchestration.
- **One execution model:** the only durable chain is `Environment -> Run -> Allocation`. `Sandbox` is the SDK primitive backed by that chain, so agent harnesses, evaluators, trainers, and data generators do not introduce special runtime lifecycles.
- **Secure remote access:** files, processes, terminals, SSH, and tunnels use explicit, revocable, task-scoped authorization.
- **Observable by default:** lifecycle state, logs, metrics, traces, inventory, usage, trajectories, and artifacts have clear owners and stable identities.
- **One production sandbox runtime:** gVisor (`runsc`) owns the production execution boundary without runtime fallback. Firecracker and Kata remain research options rather than parallel production backends.
- **Local-to-production continuity:** daily development environments exercise the same contracts used by deployed systems, with deeper Linux or cluster validation reserved for behavior that needs it.
- **Composable SDKs:** CLIs and agent products build on public APIs and SDKs; they do not become alternate control planes.

## Long-Term Capability Areas

- Programmable sandbox lifecycle and process, file, terminal, and proxy APIs.
- Agent evaluation, training, and synthetic-data workloads built from immutable environments, Run-backed sandboxes, allocation-local files, and explicit output or artifact export.
- Run status, output, resource usage, and explicit artifact delivery needed by upper-layer verification, trajectory, replay, and result systems.
- Task-scoped secrets, ephemeral filesystems, durable artifacts, controlled egress, reverse tunnels, and optional SSH access.
- Runtime templates for coding, browser, research, CI, and data workloads without marketplace or template sprawl.
- Low-cardinality usage and capacity measurements based on time, resources, execution, network, and retained control state.
- Purpose-built batch, training, RL, or experiment orchestration may be added only after its queueing, retry, checkpoint, concurrency, and budget semantics are defined; it must compose Runs and Sandboxes instead of creating a second execution substrate.

## Product Boundaries

- Axern is not a general Kubernetes replacement.
- Node-local `axctl` is operator and debugging tooling, not the product CLI.
- Agent and coding products must not depend on one provider or one coding-agent implementation.
- The core platform does not absorb application-specific protocol behavior that can be implemented through public sandbox and network primitives.
- Service, Function, Agent Profile, generic Volume, managed evaluation, and benchmark-specific lifecycles are outside the core platform. SSH and Tunnel remain explicit Allocation capabilities, not reasons to restore those product models.
- Compatibility with an early internal model is not a goal when a coherent redesign can update all in-repository consumers together.

## Applying This Direction

For a proposal, ask:

1. Which durable user capability does it add or simplify?
2. Which component owns its state and lifecycle?
3. Does it reuse the sandbox and public API model or create a parallel path?
4. What security, observability, cleanup, and usage contracts does it need?
5. Which local and production-like environments can validate it?

Keep stable object meaning and ownership in the [Domain Model](domain-model.md). Keep concrete engineering rules in the root [Agent Contract](../../AGENTS.md) and local contracts. Keep current system behavior in architecture documents, commands in runbooks or owning READMEs, and planned work in the issue tracker.
