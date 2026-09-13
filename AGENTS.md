# Axern Agent Contract

## Scope

This file defines repository-wide rules for Axern. Read the nearest subtree `AGENTS.md` and owning README before changing code there; local contracts add only subsystem-specific constraints.

## Required Context

- Use the [Module Guide](.x/module-guide.md) to locate ownership and the [Documentation Guide](docs/README.md) to locate durable product, architecture, verification, and operational contracts.
- Treat the [Stable Domain Model](docs/product/domain-model.md) as authoritative for product objects, ownership, and lifecycle meaning.
- Read the [Runtime Stack](.x/runtime-stack.md) only when a change crosses control, gateway, runtime, network, storage, or SDK boundaries.
- Read [Coding Standards](.x/coding-standards.md) for language, layering, and validation conventions.

## Platform Boundaries

- Axern is an open-source environment execution platform for agent evaluation, training, and executable data synthesis, not a general PaaS or an all-in-one benchmark, agent, or training product.
- The durable execution model is `Environment -> Run -> Allocation`. `Sandbox` is an SDK facade over that chain; terminal, process, file, SSH, and Tunnel capabilities bind to an explicit, never-reused Allocation ID.
- Higher-level evaluation, rollout, verifier, provider, budget, and training orchestration belongs above the execution platform in Axrun, Openbench, or another caller.
- Runsc is the supported production sandbox backend. Missing required isolation or platform capability must fail closed.
- PostgreSQL is the only authoritative central state backend. Before the public model stabilizes, update the initial schema and rebuild local databases instead of preserving obsolete internal schemas, protobuf gaps, aliases, or dual paths.
- Keep public contracts in `sdk/proto`, executable products in `apps/`, platform services in their owning `control/`, `gateway/`, `runtime/`, or `network/` subtree, and genuinely shared internal libraries in `lib/`.
- Keep root deployment contracts cloud-neutral; provider credentials, account setup, and regional orchestration belong outside this repository.

## Design Rules

- Prefer explicit ownership, narrow interfaces, simple data flow, and coherent long-term models over compatibility layers for uncommitted internal designs.
- Put domain behavior in the owning package and keep composition roots limited to construction and lifecycle. Do not introduce catch-all packages, helper files, or bridge aliases that hide ownership.
- When a durable contract changes, update all affected callers, generated code, tests, and authoritative documentation together.

## Validation And Documentation

- After a cohesive change, run `make verify-changed-plan` and `make verify-changed`; the repository planner selects the host-safe checks and affected heavyweight scopes.
- Run the owning Linux, Compose, kind, or regional truth path only when the changed behavior requires it. Use `make verify-full` for broad asynchronous regression and `make verify-release` for frozen release candidates.
- For protobuf changes, run generation and generated-output checks before compilation; generation replaces `sdk/go/gen` and must not run concurrently with consumers.
- Keep current commands in owning READMEs or runbooks, current cross-component behavior in architecture documents, and durable rationale in decisions. Do not place implementation history or completion notes in normative documents.
- Write English and localized Markdown prose, including list items and block quotes, as one source line per natural paragraph. Run `make agent-doc-check` after changing repository Markdown.
