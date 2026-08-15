# imagemgr

`imagemgr` is the node-local image orchestration daemon used by `axnoded` for
image-backed rootfs flows.

It exposes an HTTP-over-Unix-socket API and coordinates three mount families:

- OSS raw image: launch `imagefsd` for the raw object, then expose an ext4
  rootfs directory through `ossloop`.
- Nydus image: fetch bootstrap metadata from a registry, launch `imagefsd`, and
  mount the RAFS filesystem.
- OCI image: pull and extract layers locally, then expose a readonly overlay
  mount; `/oci_mount` can optionally auto-route to Nydus first.

## Related Docs

- Cross-component runtime log meanings: [Runtime Logs](../../docs/operations/runtime-logs.md)
- Mount routing, daemon lifecycle, and implementation ownership: [Architecture](docs/architecture.md)

## Interfaces

- Default socket: `/var/run/imagemgr.sock`
- Repo-local dev socket: `.dev/run/imagemgr.sock`
- Read-only inventory endpoint: `GET /inventory`
- Persisted mount records: `<root>/mount_records.db`

The API surface is:

- `POST /oss_mount`
- `POST /oss_umount`
- `POST /nydus_mount`
- `POST /nydus_umount`
- `POST /oci_mount`
- `POST /oci_umount`
- `POST /oci_import`
- `POST /cleanup_daemon`
- `GET /list_daemons`
- `GET /list_oci_mounts`
- `GET /list_oci_mount_details`
- `GET /inventory`

`GET /inventory` includes:

- `mounted_images`: OCI/Nydus mount records
- `imported_images`: node-local Docker archive refs available to `/oci_mount`
- `daemons`: live daemon summary plus source identity fields used for locality
- `chunkdb`: retained top-level ChunkDB aggregate summary
- `locality`: rootfs-identity entries for OCI, Nydus, and OSS/S3-backed mounts

`/oci_mount` first tries Nydus routing when a registry client is configured.
If Nydus is not detected, it falls back to the local OCI extract-plus-overlay
path.
If the requested image ref has been imported through `/oci_import`, the OCI
fallback path uses that node-local archive instead of fetching from a registry.

For private-image flows, `/oci_mount` also accepts optional request-scoped
Docker config JSON. Inline request auth overrides static auth-file entries for
that mount request only, which keeps private registry credentials
allocation-scoped instead of node-global.

## Repository Layout

- `cmd/imagemgr/`: thin daemon entrypoint
- `internal/app/`: daemon flags, logging, tracing, dependency wiring, and lifecycle
- `internal/mountstore/`: BoltDB-backed mount record persistence used by the API worker
- `api/`: Unix-socket API server plus mount request handling
- `imagefsd/`: `imagefsd` config generation, daemon lifecycle, health, and GC
- `oci/`: OCI pull, extract, overlay mount, and persistent metadata
- `nydus/`: Nydus registry fetch and bootstrap extraction
- `ossloop/`: loop-mount of raw images into directory rootfs mounts
- `configs/`: example backend templates
- `docs/architecture.md`: mount routing and implementation ownership map
- `docs/TRACING.md`: tracing and stage-timing notes

## Required Startup Inputs

Manager initialization requires all of the following inputs, even if only one mount flow is exercised in a given environment:

- `-imagefsd_bin`
- `-oss_template`
- `-nydus_template`
- `-oss_auths_path`
- `-registry_auths_path`

Common optional flags:

- `-root`: work directory for daemon state, mount records, and logs
- `-http_sock`: Unix socket path; defaults to `/var/run/imagemgr.sock`
- `-nydus_suffix`: suffix appended during Nydus auto-detection in `/oci_mount`
- `-registry_mirror_url`: dynamic registry mirror origin used by OCI pulls and
  Nydus bootstrap fetches. Nydus v2.4 lazy blob reads continue to use the
  source registry because its backend no longer supports mirror headers.
- `-debug`: enable debug logging
- `-enable_tracing`: enable timed OpenTelemetry instrumentation
- `-cgroup_memory_limit`: memory cap for launched `imagefsd` daemons
- `-nydus_readahead_workers`: bounded workers for demand-triggered Nydus cache
  readahead; zero keeps fully lazy reads
- `-nydus_readahead_window_bytes`: maximum range scheduled after a successful
  foreground read
- `-nydus_decoded_cache_bytes`: per-mount decoded chunk working-set limit;
  persisted chunks are released from this cache after ChunkDB confirms storage

Nydus launch policy is reconciled when imagemgr restores daemon metadata. If a
recovered daemon is still running with different readahead or decoded-cache
settings, imagemgr stops it and the next mount starts it with the current
policy instead of silently retaining stale launch arguments.

Common optional environment variables:

- `IMAGEMGR_INSECURE_REGISTRIES`: comma-separated registry hosts that should be
  fetched over HTTP instead of HTTPS, used by local truth environments such as
  `localhost:5001` and `host.docker.internal:5001`

Registry mirror, forward proxy, and insecure registry are separate transport
choices. A mirror rewrites requests to a trusted distribution service, a
forward proxy carries the original HTTPS registry request, and an
insecure registry explicitly opts selected hosts into plain HTTP. Registry TLS
certificate verification is enabled by default on every HTTPS path.

The production Nydus backend sends blob reads through the Dragonfly Seed Client
Service HTTP proxy. Dragonfly owns P2P task scheduling and whole-blob prefetch;
`imagefsd` owns its sparse cache. Imagemgr still resolves image metadata and the
RAFS bootstrap through its registry client. Imagefsd bounded readahead is an
optional experimental mode and stays disabled when Dragonfly prefetch is
enabled. The spawned mount process inherits standard OpenTelemetry environment
variables, so no imagemgr-specific metrics endpoint is required.

The generated Nydus registry backend keeps registry metadata, authentication,
and the initial blob request on HTTPS. HTTPS-intercepting Dragonfly proxies must
use a stable CA: Seed Client holds the CA key and signs per-origin certificates,
while imagefsd receives only the public CA certificate through `ca_cert_files`.
TLS verification remains enabled. `blob_url_scheme` controls only registry blob
redirects and may be set to HTTP after the target registry has been verified to
support it. Axern does not generate `proxy.use_http`; proxy endpoint transport
and origin URL schemes are separate concerns. Performance validation disables
proxy fallback so a direct-origin read cannot be mistaken for a Dragonfly
sample.

## Run Example

```bash
go run ./cmd/imagemgr \
  -debug \
  -root /tmp/imagemgr \
  -imagefsd_bin /usr/local/bin/imagefsd \
  -oss_template ./configs/oss_backend.json.example \
  -nydus_template ./configs/nydus_registry.json.example \
  -oss_auths_path ./oss_auths.json.example \
  -registry_auths_path ./registry_auths.json.example \
  -http_sock /tmp/imagemgr.sock \
  -nydus_suffix=-nydus \
  -cgroup_memory_limit=512MiB
```

Tracing can be enabled by adding `-enable_tracing`. See [Tracing](./docs/TRACING.md).

## API Examples

The examples below assume socket path `/tmp/imagemgr.sock`.

Mount an OSS-backed raw image and expose it as a rootfs directory:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST http://unix/oss_mount \
  -H 'Content-Type: application/json' \
  -d '{
    "endpoint": "oss-cn-hangzhou.aliyuncs.com",
    "bucket": "my-bucket",
    "object": "images/disk.raw",
    "lease_id": "example-oss-rootfs",
    "owner": "operator"
  }'
```

Mount a Nydus image directly:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST http://unix/nydus_mount \
  -H 'Content-Type: application/json' \
  -d '{"image_url":"docker.io/library/alpine:latest-nydus","lease_id":"example-nydus-rootfs","owner":"operator"}'
```

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

Unmount an OCI or Nydus-routed image mounted through `/oci_mount`:

```bash
curl --unix-socket /tmp/imagemgr.sock -X POST http://unix/oci_umount \
  -H 'Content-Type: application/json' \
  -d '{"image_url":"docker.io/library/alpine:latest","lease_id":"example-oci-rootfs"}'
```

Inspect node-local image inventory:

```bash
curl --unix-socket /tmp/imagemgr.sock http://unix/inventory
```

On a node, operators can inspect the same image cache through
`axctl image list`, `axctl image inspect <image-ref>`, and `axctl image mounts`.

The mount endpoints return:

- `mount_path`
- optional environment values needed by higher-level runtime consumers
- `immutable_mount`: a bounded source-owned descriptor containing the effective
  root, opaque identity, filesystem diagnostics, exact ordered lower paths,
  readonly state, and lease ID

The descriptor is the only image-representation hand-off to runtime rootfs
projection. Consumers validate it but do not parse OCI layers, Nydus bootstrap
state, OSS loop internals, or mountinfo to reconstruct image state.

`GET /list_oci_mount_details` returns the mounted image URL, resolved mount
path, and mount type (`oci` or `nydus`) for image-backed rootfs mounts.
It also reports `lease_count`. Mount requests require a stable `lease_id`;
repeated acquire and release calls are idempotent, and the resource is
unmounted only after its final lease is released. Failed final releases remain
durable and are retried by imagemgr reconciliation.
`POST /reconcile_mount_leases` accepts an `owner` and its complete desired
`lease_ids` set, then releases stale leases owned by that caller.

When a daemon needs to be removed explicitly, `POST /cleanup_daemon` accepts a
JSON body with `daemon_id`.

## Development And Validation

Use the repository root for the common entrypoints:

```bash
make imagemgr-build
make imagemgr-test
```

Package-focused checks are useful while iterating:

```bash
go test ./api ./imagefsd ./oci
```

For Linux truth validation and debugging, use the shared node-runtime workspace:

```bash
make devbox-up

# Inside the Linux workspace
make node-dev-prepare
make imagefsd-build
make imagemgr-dev-run
```

The same workspace is used for the end-to-end image-backed rootfs checks:

```bash
make axnoded-verify-node-oci-e2e
make axnoded-verify-node-nydus-e2e
make axnoded-verify-node-oss-e2e
```

On macOS, treat `make imagemgr-test` and `make imagemgr-build` as the best
available local checks. FUSE, loop-mount, and overlay-mount correctness must be
validated in a Linux workspace.
