# Axrun Agent Contract

## Purpose

`apps/axrun` is Axern's native agent harness, task runner, verifier, and trajectory-capture CLI. It consumes public Axern APIs and remains above the execution platform. Use the [Axrun README](README.md) to route detailed architecture, domain-model, rollout-evidence, usage, and acceptance work.

## Ownership Boundaries

- Keep `internal/cliapp` as the composition root, `internal/commands/<domain>` as CLI adapters, and `internal/application/<domain>` as workflow orchestration.
- Keep native records and invariants in `internal/domain` and `internal/contract`, compiled task inputs in `internal/taskset`, rollout lifecycle in `internal/rollout`, backend adapters in `internal/backend`, and Axern/local execution adapters in `internal/sandbox`.
- Use public Axern SDK/API surfaces only. Do not import control-plane or runtime internals, node lifecycle APIs, database adapters, or implementation-only protos.
- Keep execution compiled-only: rollout consumes a frozen TaskSet descriptor and native `TaskInstance` records.
- Keep task runtime images separate from read-only agent/tool images, and keep agent implementations behind focused adapters rather than in the rollout engine.
- Keep Axrun an atomic execution, verification, and trajectory-capture capability. Benchmark suites, seed generation, provider policy, and training orchestration belong to callers.
- Axern-backed execution must follow the platform [Stable Domain Model](../../docs/product/domain-model.md); Axrun must not create a second control plane.

## Validation

- Run `go test ./apps/axrun/...`, `go vet ./apps/axrun/...`, `test -z "$(gofmt -l apps/axrun)"`, and `make axrun-local-smoke`.
- Run `make axrun-verify` for release-level Axrun changes.
