# Image Manager Agent Contract

## Purpose

`runtime/imagemgr` owns image-mount orchestration. Use its [README](README.md) and [architecture](docs/architecture.md) for interfaces and commands.

## Ownership Boundaries

- Keep one OCI image path. `api` owns mount requests and resource leases; `oci` owns extraction, overlay mounts and backend recovery; registry transport and authentication belong in their support packages.
- Reject unsupported image formats and retained authority before recovery. Do not reinterpret them as ordinary OCI data or discard ownership records to make startup succeed.
- Socket, request, authentication and work-directory changes are contracts with axnoded and the Linux stack. Keep persistence out of API types and mount policy out of callers.

## Validation

Run `make imagemgr-test` and the build, architecture, or Linux mount checks selected by `make verify-changed`.
