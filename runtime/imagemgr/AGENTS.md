# Image Manager Agent Contract

## Purpose

`runtime/imagemgr` owns image-mount orchestration. Use its [README](README.md) and [architecture](docs/architecture.md) for interfaces and commands.

## Ownership Boundaries

- Imagemgr orchestrates mounts but does not own OCI extraction, imagefsd data, or registry behavior.
- Route ordinary OCI images through the OCI path and Nydus images through imagefsd unless an explicit capability requires another design.
- Socket, request, authentication, work-directory, and launch changes are contracts with axnoded and the Linux stack. Keep persistence out of API types and mount policy out of callers.

## Validation

Run `make imagemgr-test` and the build, architecture, or Linux mount checks selected by `make verify-changed`.
