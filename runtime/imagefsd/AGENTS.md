# Image Filesystem Agent Contract

## Purpose

`runtime/imagefsd` owns read-only image filesystem serving and chunk reuse. Read the [Image Filesystem README](README.md) and [architecture](docs/architecture.md) for commands, mount flow, and implementation routing.

## Ownership Boundaries

- Keep the binary and CLI surface in `src/main.rs` and `src/cli.rs`, filesystem behavior in `src/fs.rs` and `src/image`, and backend/cache/dedup/peer behavior in `src/backend`.
- Keep raw-image and Nydus implementations separate. Raw-image dedup identity includes the configured image name and must remain aligned with callers and persisted metadata.
- Treat CLI flags, chunk protocol, socket paths, and work-directory layout as contracts with imagemgr and the Linux development stack.
- Keep Redis-backed discovery optional and keep filesystem correctness independent from peer availability.

## Validation

- Run `cargo fmt --all --check` and `make imagefsd-test`; add `make imagefsd-build` for CLI, backend, cache, or image behavior.
- Run the Redis integration tests when Redis discovery or chunk indexing changes.
- Validate FUSE, mount, and chunk-server behavior in Linux through the relevant target selected by `make verify-changed`; macOS checks do not prove those paths.
