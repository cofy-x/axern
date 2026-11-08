# Axern Agent Contract

## Platform Boundaries

- Axern is a self-hosted execution platform for AI agents. Prioritize Linux single-host serving and continued agent work; macOS is a client and Linux-VM development host. See the [Product Direction](docs/product/product-direction.md) and [Single-Host Architecture Decision](docs/decisions/single-host-agent-serving.md) for target scope versus implemented capabilities.
- The durable execution model is `Environment -> Run -> Allocation`. `Sandbox` is an SDK facade over that chain; terminal, process, file, SSH, and Tunnel capabilities bind to an explicit, never-reused Allocation ID.
- Caller workflows, agent planning, memory, messages, approvals, and semantic retries remain outside Axern. Workspace is an independent retained-data identity with one fenced writer, not another execution lifecycle. Optional persistent mounts never redirect Allocation-scoped handles or become a second source of execution authority.
- gVisor/runsc is the sole long-term sandbox backend. Do not introduce another runtime, runtime registry, or fallback. Missing required isolation or platform capability must fail closed.
- PostgreSQL is the only authoritative central state backend. Internal schema changes must update the owning model atomically; do not preserve obsolete internal tables, protobuf gaps, aliases, dual reads, or dual writes unless a published compatibility contract explicitly requires them.
- Keep root deployment contracts cloud-neutral; provider credentials, account setup, and regional orchestration belong outside this repository.

## Working Rules

- Read the applicable subtree `AGENTS.md`; use the [Module Guide](.x/module-guide.md) to locate owners, [Coding Standards](.x/coding-standards.md) for implementation rules, and the [Stable Domain Model](docs/product/domain-model.md) for public identity or lifecycle changes.
- Keep behavior in its owning package, depend on narrow capabilities, and do not add catch-all helpers, bridge aliases, or parallel lifecycle paths.
- Change durable contracts atomically across callers, generated code, tests, and their single authoritative document.

## Validation And Documentation

- Run `make verify-changed-plan` and `make verify-changed`; the [Verification Tiers](docs/verification/local-full-verification.md) define broader gates. Protobuf generation must finish before consumers run.
- Keep commands in READMEs or runbooks, current behavior in architecture documents, durable rationale in decisions, and history only in release notes. Run `make agent-doc-check` for Markdown changes.
