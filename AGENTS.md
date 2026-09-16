# Axern Agent Contract

## Scope

This file defines repository-wide rules for Axern. Read the applicable subtree `AGENTS.md` before changing code there; local contracts add only subsystem-specific constraints.

## Task-Scoped Reading

- Use the [Module Guide](.x/module-guide.md) when locating an owner. Once the owner is known, follow its local contract's task routes; the owning README is an index and command reference, not a mandatory cover-to-cover dependency.
- Read [Coding Standards](.x/coding-standards.md) when implementing code. Read the [Stable Domain Model](docs/product/domain-model.md) when changing product objects, API semantics, persistence ownership, or lifecycle meaning.
- Use the [Runtime Stack](.x/runtime-stack.md) for cross-component changes and the [Documentation Guide](docs/README.md) for other contract lookup. Read the contracts selected by the task, not every linked document recursively.
- Before editing a document, read it and its applicable rules. Do not load unrelated architecture, deployment, or release documents for a local implementation change.

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

- After a cohesive change, run `make verify-changed-plan` and `make verify-changed`. The [Verification Tiers](docs/verification/local-full-verification.md) own check selection and delivery-stage requirements; local contracts add only subsystem-specific checks. Run `make agent-doc-check` for Markdown edits; this does not replace checks selected for documentation builds or tooling changes.
- For protobuf changes, run generation and generated-output checks before compilation; generation replaces `sdk/go/gen` and must not run concurrently with consumers.
- Keep current commands in owning READMEs or runbooks, current cross-component behavior in architecture documents, and durable rationale in decisions. Do not place implementation history or completion notes in normative documents.
- Follow the [Markdown Formatting](.x/coding-standards.md#markdown-formatting) rules for documentation edits. Keep one authoritative explanation of each contract and link to it from task routes rather than copying it into agent rules.
