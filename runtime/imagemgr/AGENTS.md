# Image Manager Agent Contract

## Purpose

`runtime/imagemgr` owns image-mount orchestration. Read the [Image Manager README](README.md) and [architecture](docs/architecture.md) for interfaces, commands, and mount routing.

## Ownership Boundaries

- Keep `cmd/imagemgr` thin, process wiring in `internal/app`, the Unix-socket contract and request orchestration in `api`, and durable mount records in `internal/mountstore`.
- Imagemgr orchestrates mounts but does not own image data planes: OCI extraction/overlay belongs to `oci`, imagefsd lifecycle to `imagefsd`, and shared Nydus/registry behavior to `nydus` and `pkg/imageregistry`.
- Route ordinary OCI images through the OCI path and Nydus images through imagefsd unless an explicit capability requires another design.
- Treat socket names, work directories, request shapes, registry authentication, and imagefsd launch flags as cross-subsystem contracts with axnoded and the Linux development stack.
- Keep persistence details out of API types and keep mount policy out of callers.

## Validation

- Run `make imagemgr-test`; add `make imagemgr-build` for API, daemon, registry, or OCI changes and `make imagemgr-check-architecture` for package-boundary changes.
- Validate mount, loop, cgroup, and imagefsd-launch behavior in Linux through the relevant target selected by `make verify-changed`; macOS checks do not prove those paths.
