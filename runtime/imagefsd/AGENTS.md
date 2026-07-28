# AGENTS.md

## Purpose

This file is the agent contract for `imagefsd`.

Use it to answer four questions quickly before making changes:

- which layer owns the behavior
- which filesystem and dedup constraints are hard boundaries
- which validation level matches the change
- which downstream docs or runtime consumers must stay in sync

Keep this file short and rule-oriented. Put background material in:

- [Image Filesystem Daemon README](README.md) for commands and runtime behavior
- [Architecture](docs/architecture.md) for mount flow, chunk serving, and implementation ownership

## Hard Architecture Constraints

- The binary entry stays in [`src/main.rs`](src/main.rs), and the CLI surface stays in [`src/cli.rs`](src/cli.rs).
- Read-only filesystem behavior belongs in [`src/fs.rs`](src/fs.rs) plus the image-specific implementations under [`src/image`](src/image).
- Backend, cache, dedup, chunk database, and peer logic belong under [`src/backend`](src/backend). Do not move those responsibilities into the CLI layer.
- Raw-image behavior and Nydus behavior are intentionally separate:
  - raw-image mount logic belongs in [`src/image/raw.rs`](src/image/raw.rs)
  - Nydus filesystem behavior belongs in [`src/image/nydus.rs`](src/image/nydus.rs)
- Dedup metadata identity depends on `--name` for raw-image flows. Treat changes to that meaning as compatibility changes that require doc and caller review.
- Chunk-server compatibility matters outside this crate. `imagemgr` and the repo-local Linux workspace depend on the serve-chunk flow and socket usage.

## Change-to-Validation Matrix

Prefer root `make` targets when they exist.

### Generic Rust logic only

- Must run:
  - `cargo fmt --all --check`
  - `make imagefsd-test`

### CLI, backend, cache, or image behavior changes

- Must run:
  - `cargo fmt --all --check`
  - `make imagefsd-test`
- Should run:
  - `make imagefsd-build`

### Redis-backed peer discovery or chunk-index changes

- Must run:
  - `cargo fmt --all --check`
  - `make imagefsd-test`
- Should run when the affected logic uses Redis integration:
  - `cargo test -p imagefsd --features redis-integration-tests redis_ -- --test-threads=1`

### Linux-only FUSE or chunk-server behavior changes

- Must run:
  - `cargo fmt --all --check`
  - `make imagefsd-test`
- Should run in a Linux workspace:
  - `make imagefsd-build`
  - `make imagefsd-dev-serve-chunk`
- If the change affects how `imagemgr` launches or talks to `imagefsd`, also validate the relevant `imagemgr` flow.

### Non-Linux fallback rule

- If Linux-only validation is unavailable, say so explicitly in the handoff.
- In that case, do the best available checks, usually formatting, unit tests, and a debug build.
- Do not claim FUSE-runtime correctness from macOS-only validation.

## Required Sync Points

### CLI or flag-shape changes

- Update the [Image Filesystem Daemon README](README.md).
- Update callers in `runtime/imagemgr` if daemon launch flags or socket usage change.
- Update the [Runtime Stack](../../.x/runtime-stack.md) if the change affects cross-subsystem behavior.

### Chunk server or repo-local socket changes

- Update root Linux-workspace docs and scripts.
- Update `runtime/imagemgr` docs if that flow depends on the changed behavior.

### Mount semantics or config-meaning changes

- Update [Image Filesystem Daemon README](README.md) examples and constraints.
- Update affected tests in [`tests`](tests) and any inline unit tests under [`src`](src).

## Environment Caveats

- Truth validation for `mount` requires Linux plus FUSE support.
- The recommended truth environment is the repository-root Linux workspace started via `make devbox-up`.
- The repo-local Linux workflow expects the chunk server socket at `.dev/run/imagefsd-chunk.sock` and chunk data under `.dev/imagefsd/chunkdb`.

## Cross-Subsystem Dependencies

- `imagemgr` launches `imagefsd` and relies on compatible CLI flags and daemon behavior for OSS and Nydus flows.
- The repo-local Linux workflow and runtime-stack routing are described in the [Runtime Stack](../../.x/runtime-stack.md).

## Task Entry Points

- Use the [CLI Overview](README.md#cli-overview) for supported commands and [Development](README.md#development) for development checks.
- Start binary and CLI work in [`src/main.rs`](src/main.rs) and [`src/cli.rs`](src/cli.rs), filesystem behavior in [`src/fs.rs`](src/fs.rs), image behavior in [`src/image`](src/image), backend/dedup/chunk work in [`src/backend`](src/backend), and integration coverage in [`tests`](tests).
