# Coding Standards

Rules and conventions for Axern contributors and agents.

## Working Rules

- Keep the repository platform-oriented, not template-oriented.
- Keep Go, Rust, TypeScript, and Python as first-class workspaces.
- Put runnable entrypoints in `apps/`, external SDKs and proto contracts in `sdk/`, internal shared libraries in `lib/`, and runtime-owned Rust code under its active runtime subtree.
- Treat `runtime/`, `network/`, and `control/` as platform areas, not dump zones for demos or placeholder apps.
- Keep reserved paths intentionally minimal until real platform code is ready.

## Design Judgment

- Prefer simple data flow and explicit ownership over premature abstraction.
- Add abstractions only when they clarify a real domain boundary, remove
  meaningful duplication, or reduce maintenance risk.
- Follow existing subsystem patterns before introducing a new style or helper
  layer.
- Keep compatibility layers out of new designs unless an external contract
  requires them.

## Go Interface Rules

- Define Go interfaces at the consumer boundary. Prefer the smallest capability interface that the caller actually needs.
- Do not pass broad "god interfaces" through application wiring when smaller role-specific interfaces will do.
- Do not rely on type assertions to discover required behavior on the main path. If a capability is required for normal control flow, model it as an explicit interface dependency.
- Do not downcast from an interface back to a concrete implementation in order to continue normal application flow. If implementation-specific behavior is genuinely required, introduce a narrow interface for that behavior instead.
- Keep composition roots and API layers dependent on capabilities, while concrete stores and services implement those capabilities behind the boundary.

## Placement By Language

| Language | Canonical location | Notes |
| :--- | :--- | :--- |
| Go | active `go.work` members | Put product CLI code in `apps/cli`, Go SDK code in `sdk/go`, internal shared libraries in `lib/go`, and platform services in their owning `control/`, `gateway/`, `runtime/`, or `network/` subtree. |
| Rust | `runtime/imagefsd` | Add new Rust workspace members only with a concrete platform owner and root docs updates. |
| TypeScript | `sdk/typescript` | Use ESM only. |
| Python | `sdk/python/src` | Keep importable modules under `src/`. |

## Validation Baseline

- Prefer root `make` targets when they exist for the scope of the change.
- Changes to repository Markdown should run `make agent-doc-check`.
- Cross-workspace or root orchestration changes should run `make build`, `make test`, and `make lint`.
- Go changes should run the relevant package tests, subsystem validation, or the root `make test` for the affected `go.work` member.
- Rust changes should run `cargo fmt --all --check` and `cargo test --workspace -- --test-threads=1`.
- TypeScript changes under `sdk/typescript` should run `make sdk-typescript-verify`.
- Python changes under `sdk/python` should run `make test-py` and `make lint-py`; run `uv build sdk/python` when package metadata or distribution behavior changes.
- Shared protobuf contract changes should run `make protos`, `make proto-generated-check`, and `make -C sdk/proto lint`. Run generation and generated-output checks before Go compilation, never in parallel with it, because the generator replaces `sdk/go/gen` atomically at the workflow level rather than file by file.

## Repository Hygiene

- Do not reintroduce removed template apps or dashboard code without an explicit product requirement.
- Keep all code and documentation in English.
- Follow the sync rules in the [Agent Contract](../AGENTS.md) when workspace, root orchestration, or top-level platform areas change.
