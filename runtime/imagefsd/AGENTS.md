# Image Filesystem Agent Contract

## Purpose

`runtime/imagefsd` owns read-only image filesystem serving and chunk reuse. Use its [README](README.md) and [architecture](docs/architecture.md) for commands and design.

## Ownership Boundaries

- Keep raw-image and Nydus behavior separate and preserve their documented cache identities.
- CLI, chunk protocol, socket, and work-directory changes are contracts with imagemgr and the Linux stack.
- Keep Redis-backed discovery optional and keep filesystem correctness independent from peer availability.

## Validation

Run `cargo fmt --all --check`, `make imagefsd-test`, and the build, Redis, or Linux checks selected by `make verify-changed` for the affected path.
