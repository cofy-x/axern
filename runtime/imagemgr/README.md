# imagemgr

`imagemgr` is the node-local image orchestration daemon used by `axnoded` for image-backed rootfs flows.

It exposes an HTTP-over-Unix-socket API for OCI image pulls, Docker archive imports, read-only overlay mounts and successful-Run rootfs publication. Mount leases own resource lifetime; stopping the process preserves mounted resources and recovery records.

## Related Docs

- Cross-component runtime log meanings: [Runtime Logs](../../docs/operations/runtime-logs.md)
- Mount leases, recovery, and implementation ownership: [Architecture](docs/architecture.md)
- Public data and rootfs-publication boundaries: [Storage Architecture](../../docs/architecture/storage-architecture.md)

## Interfaces

- Default socket: `/var/run/imagemgr.sock`
- Repo-local dev socket: `.dev/run/imagemgr.sock`
- Read-only inventory endpoint: `GET /inventory`
- Authoritative resource and lease records: `<root>/resource_leases.db`
- OCI recovery/refcount projection: `<root>/oci/metadata.db`

The API surface is:

- `POST /oci_mount`
- `POST /oci_umount`
- `POST /oci_import`
- `POST /reconcile_mount_leases`
- `POST /rootfs_snapshot`
- `GET /oci_resolve?ref=<image-ref>`
- `GET /list_oci_mounts`
- `GET /list_oci_mount_details`
- `GET /inventory`

`GET /inventory` includes:

- `mounted_images`: OCI mount records and lease counts
- `imported_images`: node-local Docker archive refs available to `/oci_mount`

`/oci_mount` resolves an imported archive by image reference when available; otherwise it pulls the OCI image from a registry. `/oci_resolve` returns the canonical reference, cache identity and whether imported content is available. Unsupported image formats are rejected before content is published as a workload rootfs.

For private-image flows, `/oci_mount` also accepts optional request-scoped Docker config JSON. Inline request auth overrides static auth-file entries for that mount request only, which keeps private registry credentials allocation-scoped instead of node-global.

## Repository Layout

- `cmd/imagemgr/`: thin daemon entrypoint
- `internal/app/`: daemon flags, logging, tracing, dependency wiring, and lifecycle
- `internal/mountstore/`: BoltDB-backed mount record persistence used by the API worker
- `api/`: Unix-socket API server plus mount request handling
- `oci/`: OCI pull, extract, overlay mount, and persistent metadata
- `pkg/imageregistry/`, `pkg/registryauth/`: registry transport, credential parsing and matching
- `configs/README.md`: registry configuration guide
- `docs/architecture.md`: mount routing and implementation ownership map
- `docs/TRACING.md`: tracing and stage-timing notes

## Configuration And Startup

Run from this module with a fresh, owned directory:

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr-oci \
  -http_sock /tmp/imagemgr-oci.sock
```

The supported flags are:

| Flag | Meaning |
| --- | --- |
| `-root` | Work directory for resource authority, OCI metadata and logs; defaults to `.`. |
| `-http_sock` | Unix socket path; defaults to `/var/run/imagemgr.sock`. |
| `-registry_auths_path` | Optional static registry credentials file; see [configuration](configs/README.md). |
| `-registry_mirror_url` | Optional registry mirror origin for OCI pulls. |
| `-snapshot_repository` | Platform-owned OCI repository for immutable Allocation rootfs publication; publication is unavailable when unset. |
| `-debug` | Enable debug logging. |
| `-enable_tracing` | Enable shared OpenTelemetry instrumentation; see [Tracing](docs/TRACING.md). |

`IMAGEMGR_INSECURE_REGISTRIES` explicitly selects comma-separated registry hosts for HTTP transport. Standard proxy environment variables configure forward-proxy transport.

Registry mirror, forward proxy, and insecure registry are separate transport choices. A mirror rewrites requests to a trusted distribution service, a forward proxy carries the original HTTPS registry request, and an insecure registry explicitly opts selected hosts into plain HTTP. Registry TLS certificate verification is enabled by default on every HTTPS path.

Startup rejects unsupported persisted resource types, corrupt lease authority and retained retired-backend state before OCI recovery. These records may still own mounts or bytes. Preserve the matching installation and resolve ownership explicitly; do not remove state to make a new binary start. Native installation and maintenance boundaries are defined by the [installation guide](../../deploy/standalone/INSTALL.md).

## API Examples

The examples below assume socket path `/tmp/imagemgr.sock`.

Mount an image through the OCI entrypoint:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST http://unix/oci_mount \
  -H 'Content-Type: application/json' \
  -d '{"image_url":"docker.io/library/alpine:latest","lease_id":"example-oci-rootfs","owner":"operator"}'
```

Import a Docker archive for later `/oci_mount` by the same ref:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST 'http://unix/oci_import?ref=myapp:dev' \
  -H 'Content-Type: application/x-tar' \
  --data-binary @/tmp/myapp.tar
```

Release the OCI mount lease:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST http://unix/oci_umount \
  -H 'Content-Type: application/json' \
  -d '{"image_url":"docker.io/library/alpine:latest","lease_id":"example-oci-rootfs"}'
```

Inspect node-local image inventory:

```bash
curl --unix-socket /tmp/imagemgr.sock http://unix/inventory
```

On a node, operators can inspect the same image cache through `axctl image list`, `axctl image inspect <image-ref>`, and `axctl image mounts`.

The mount endpoints return:

- `mount_path`
- optional environment values needed by higher-level runtime consumers
- `immutable_mount`: a bounded source-owned descriptor containing the effective root, opaque identity, filesystem diagnostics, exact ordered lower paths, readonly state, and lease ID

The response also includes resolved OCI entrypoint, command, working-directory and user metadata when available. The immutable descriptor is the image-representation hand-off to runtime rootfs projection; consumers validate it without reconstructing image state from OCI layers or mountinfo.

`GET /list_oci_mount_details` returns the image URL, cache key, resolved mount path, `oci` mount type and `lease_count`. Mount requests require a stable `lease_id`; repeated acquire and release calls are idempotent, and the resource is unmounted only after its final lease is released. Failed final releases remain durable and are retried by imagemgr reconciliation. `POST /reconcile_mount_leases` accepts an `owner` and its complete desired `lease_ids` set, then releases stale leases owned by that caller.

`POST /rootfs_snapshot` receives multipart `metadata` followed by a streamed `upper_layer`. It converts the stopped Allocation's writable rootfs layer and publishes an OCI image into `-snapshot_repository`. The request supplies bytes and frozen base-image identity, never a source host path. Controld owns successful-Run admission and publication of the resulting Environment; imagemgr does not introduce another public Snapshot identity.

Normal daemon shutdown preserves OCI mounts and their recovery records. Closing the OCI manager is not a lease release; unmounting remains owned by explicit final-lease cleanup. Restart reconciles those records with actual kernel mount state. This does not promise that mounts or workload processes survive a host reboot.

## Development And Validation

Use the repository root for the common entrypoints:

```bash
make imagemgr-build
make imagemgr-test
```

Package-focused checks are useful while iterating:

```bash
go test ./api ./oci ./pkg/...
```

For Linux truth validation and debugging, use the shared node-runtime workspace:

```bash
make devbox-up

# Inside the Linux workspace
make node-dev-prepare
make imagemgr-dev-run
```

The same workspace is used for the end-to-end image-backed rootfs checks:

```bash
make axnoded-verify-node-oci-e2e
```

Use `make imagemgr-check-architecture` for package boundaries and the checks selected by `make verify-changed` for integration coverage. On macOS, `make imagemgr-test` and `make imagemgr-build` provide host-safe feedback; overlay-mount correctness requires Linux validation.
