# Axern Product Direction

This document defines the durable product direction used to accept or reject new scope. It is not a roadmap or backlog.

## North Star

Axern is an open-source environment execution platform for agent evaluation, training, and data synthesis. Its public SDK provides secure, rebuildable, high-concurrency sandboxes with explicit lifecycle, access, output, and cleanup contracts.

## Principles

- **One execution model:** `Environment -> Run -> Allocation -> runsc sandbox` is the only durable chain; `Sandbox` is an SDK facade over it.
- **One production runtime:** runsc owns the execution boundary. Missing isolation or capability evidence fails closed; experimental runtimes do not create parallel product models.
- **Explicit authority:** process, file, archive, terminal, SSH, Tunnel, and output access are finite and Allocation-scoped.
- **Durable control, local execution:** PostgreSQL owns central product intent; node runtime and kernel state remain local, recoverable projections.
- **Rebuildable inputs, explicit outputs:** Environments are immutable, writable state is Allocation-local, and selected outputs use bounded delivery to caller-owned storage.
- **Public composition:** CLIs and SDK consumers use released APIs rather than internal packages or alternate control planes.
- **Local-to-deployed continuity:** development and deployed environments exercise the same contracts; Linux- or cluster-specific behavior is proven in the environment that owns it.

## Product Boundary

Axern owns Namespace, Environment, Run, Allocation, Node, Secret, authorization, resource admission, execution leases, capability evidence, Allocation operations, TunnelSession, and bounded output delivery.

Axern does not own application orchestration, service deployment, persistent workspaces, volume provisioning, clusters, regions, cloud accounts, or image builds. Those capabilities belong to callers, applications, storage systems, or deployment infrastructure.

SSH, Terminal, and Tunnel are execution-platform capabilities, not reasons to introduce service deployment or application-session models. Node-local operator tools are diagnostics, not public lifecycle authorities.

## Scope Test

A proposal belongs in Axern only when it serves environment execution, has one explicit state owner, composes the stable Run and Allocation lifecycle, defines authorization and cleanup, and cannot be implemented safely through existing public primitives. Otherwise it belongs above or below the platform.

Stable object meaning lives in the [Domain Model](domain-model.md). Current implementation belongs in architecture documents, commands in runbooks or READMEs, and version history in release notes.
