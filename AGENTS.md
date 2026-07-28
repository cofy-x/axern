# Axern Agent Contract

## Scope

This file defines repository-wide rules for `axern`. Apply it to every task,
then read the nearest subtree `AGENTS.md` before changing code there. A local
contract may add stricter rules but cannot override this file.

## Minimum Context

Read only the context required for the task:

1. Use the [Module Guide](.x/module-guide.md) to find the owning subtree. Read
   its `AGENTS.md` when present and its `README.md`.
2. Read [Project Overview](.x/project-overview.md) for root layout, workspace,
   build orchestration, or placement changes.
3. Read [Runtime Stack](.x/runtime-stack.md) only when behavior crosses service,
   SDK, storage, runtime, gateway, or network boundaries.
4. Read [Coding Standards](.x/coding-standards.md) for language rules and the
   validation baseline.
5. Use the [Documentation Guide](docs/README.md) to find product direction,
   architecture, verification, and operational material.

Do not read every repository document by default. Subsystem-local work should
normally need this file plus the local contract and README.

## Sources Of Truth

When documentation disagrees with executable configuration or code, verify the
behavior and fix the stale document in the same change.

| Concern | Source of truth |
| :--- | :--- |
| Workspace membership | `go.work`, `Cargo.toml`, `pnpm-workspace.yaml`, `pyproject.toml` |
| Root commands | `Makefile`, `mk/*.mk`, `make help` |
| Public and internal RPC shapes | `sdk/proto` and generated-code checks |
| Subsystem ownership and validation | nearest `AGENTS.md`; owning `README.md` when no local contract exists |
| User and operator workflows | owning `README.md` or `docs/operations/` runbook |
| Cross-subsystem runtime boundaries | `.x/runtime-stack.md` |
| Long-term product direction | `docs/product/product-direction.md` |

## Repository Constraints

- `apps/` contains executable product entrypoints.
- `control/`, `gateway/`, `runtime/`, and `network/` contain platform services
  and data planes owned by those areas.
- `sdk/` contains public language SDKs and protobuf contracts; it does not
  contain product applications.
- `lib/` contains repository-internal libraries shared by multiple Axern
  modules. Do not move single-module helpers there.
- Keep Go, Rust, TypeScript, and Python as first-class workspaces. Add a
  workspace member only when it has a concrete owner and update its workspace
  configuration and relevant docs together.
- Do not reintroduce template frontend, dashboard, Tauri, or unrelated demo
  applications without an explicit product requirement.
- Keep root deployment commands and `deploy/helm/` cloud-neutral. Provider-
  specific values, credentials, cluster paths, and cloud resource orchestration
  belong in an external workspace or provider repository.

## Design Policy

Axern is in active development. Prefer a coherent long-term model over a
compatibility layer for an early internal design. Preserve compatibility only
for a concrete external contract or when the user explicitly requests it.

Keep domain ownership explicit, interfaces narrow, and data flow simple. A
root-cause fix may update interfaces, schemas, control flow, or module
boundaries when all affected callers, tests, generated code, and docs are
updated together.

## Completing A Change

- Use the narrowest relevant validation from the local contract. Use root
  `make` targets for cross-workspace changes.
- For protobuf changes, run generation and generated-output checks before Go
  compilation; generation replaces `sdk/go/gen` and must not run in parallel
  with compilation.
- Update documentation only when a durable contract, workflow, or user-visible
  behavior changed. Do not add completion notes or implementation history to
  normative docs; Git history already records the process.
- If workspace membership changes, update the owning workspace file and the
  module guide when routing or ownership changed.
- If a public workflow or top-level area changes, update the root `README.md`
  and the owning document.
- If a cross-subsystem API, socket, lifecycle, or ownership boundary changes,
  update `.x/runtime-stack.md` and every affected local contract or README.
- After changing repository Markdown, run `make agent-doc-check`.
