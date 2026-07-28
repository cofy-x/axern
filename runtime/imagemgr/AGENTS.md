# AGENTS.md

## Purpose

This file is the agent contract for `imagemgr`.

Use it to answer four questions quickly before making changes:

- which package owns the behavior
- which cross-subsystem boundaries are hard constraints
- which validation level matches the change
- which related docs, examples, or runtime consumers must stay in sync

Keep this file short and rule-oriented. Put background material in:

- [Image Manager README](README.md) for interfaces, commands, and runtime behavior
- [Architecture](docs/architecture.md) for mount routing and implementation ownership
- [Tracing](docs/TRACING.md) for tracing-specific guidance

## Hard Architecture Constraints

- Keep [`cmd/imagemgr`](cmd/imagemgr) as a thin daemon entrypoint.
- Keep process wiring, flag parsing, logging, tracing, and shutdown orchestration in [`internal/app`](internal/app).
- The Unix-socket API surface belongs in [`api`](api). Do not move mount policy into callers or duplicate request validation in unrelated packages.
- Persisted mount records belong in [`internal/mountstore`](internal/mountstore). Keep HTTP request/response types in [`api`](api), and do not put BoltDB persistence details there.
- `imagemgr` owns mount orchestration, not the image data plane:
  - OCI extraction and overlay logic belong in [`oci`](oci)
  - imagefsd process lifecycle belongs in [`imagefsd`](imagefsd)
  - OSS raw-image to directory exposure belongs in [`ossloop`](ossloop)
- OCI image mounts must stay in [`oci`](oci). Do not route standard OCI images through `imagefsd` unless the feature explicitly requires it.
- Nydus and registry-auth behavior should reuse [`nydus`](nydus) and [`pkg/imageregistry`](pkg/imageregistry). Do not add a second registry-auth parsing path elsewhere.
- OSS rootfs flow is intentionally two-stage: raw image mount through `imagefsd`, then loop-mount to a directory rootfs. Preserve that separation unless the feature intentionally redesigns the flow.
- Socket and workdir expectations are shared with the root Linux dev workflow and with `axnoded`. Treat changes to socket names, `.dev/` layout, or default paths as cross-subsystem changes.

## Change-to-Validation Matrix

Prefer root `make` targets when they exist.

### Generic Go logic only

- Must run:
  - `make imagemgr-test`
- Should run:
  - targeted `go test` for affected packages

### API, daemon manager, registry, or OCI behavior changes

- Must run:
  - `make imagemgr-test`
- Should run:
  - `make imagemgr-build`

### Linux-only mount, loop-mount, cgroup, or imagefsd-launch changes

- Must run:
  - `make imagemgr-test`
- Should run in a Linux workspace:
  - `make node-dev-prepare`
  - `make imagefsd-build`
  - `make imagemgr-dev-run`
- If the change affects a socket path or image-backed rootfs behavior used by `axnoded`, also run the relevant `axnoded` validation for that workflow.

### Non-Linux fallback rule

- If Linux-only truth validation is unavailable, say so explicitly in the handoff.
- In that case, perform the best available checks, usually `make imagemgr-test` plus `make imagemgr-build`.
- Do not treat missing FUSE, loop, or overlay capabilities on macOS as product failures.

## Required Sync Points

### API and request-shape changes

- Update [`api/types.go`](api/types.go) and related tests under [`api`](api).
- Update [Image Manager README](README.md) examples if user-facing request or response shape changes.
- Keep daemon ID generation compatibility in mind when changing request fields or config semantics in [`api/worker.go`](api/worker.go) and the mount flow files under [`api`](api).

### imagefsd invocation or workdir-layout changes

- Update the [Image Manager README](README.md).
- Update the [Runtime Stack](../../.x/runtime-stack.md) if the change affects cross-subsystem behavior.
- Update root Linux-workspace docs or scripts if `.dev/` paths or socket names change.

### Config, auth, or template changes

- Update example files under [`configs`](configs).
- Update [`oss_auths.json.example`](oss_auths.json.example) and [`registry_auths.json.example`](registry_auths.json.example) when the example shape changes.
- Update the [Image Manager README](README.md) if required startup inputs or flags change.

## Environment Caveats

- The real mount environment is Linux. Loop mounts, overlay mounts, and FUSE behavior are not meaningfully validated on macOS.
- The recommended truth environment is the repository-root Linux workspace started via `make devbox-up`.
- The repo-local dev workflow expects:
  - socket at `.dev/run/imagemgr.sock`
  - state under `.dev/imagemgr`
  - `imagefsd` binary at `target/debug/imagefsd`

## Cross-Subsystem Dependencies

- `axnoded` consumes `imagemgr` through the Unix socket API for image-backed rootfs flows.
- `imagemgr` launches and supervises `imagefsd` daemons for OSS and Nydus flows, and depends on `imagefsd` CLI flags remaining compatible.
- Cross-runtime routing rules live in the [Runtime Stack](../../.x/runtime-stack.md).

## Task Entry Points

- Use [Repository Layout](README.md#repository-layout) for the maintained code-layout map.
- Run `make imagemgr-check-architecture` after directory or package-boundary changes.
- Start daemon wiring and flags in [`internal/app`](internal/app), API and mount orchestration in [`api`](api), mount persistence in [`internal/mountstore`](internal/mountstore), `imagefsd` process lifecycle in [`imagefsd`](imagefsd), OCI image work in [`oci`](oci), Nydus/registry work in [`nydus`](nydus) and [`pkg/imageregistry`](pkg/imageregistry), and OSS directory exposure in [`ossloop`](ossloop).
