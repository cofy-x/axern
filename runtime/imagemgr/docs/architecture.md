# Imagemgr Architecture

`imagemgr` is the node-local image orchestration daemon used by `axnoded`.
It owns API-level mount routing and process orchestration, while image bytes,
filesystem reads, loop mounts, and OCI extraction stay in their owning packages.

Use this document when changing mount routing, daemon lifecycle, inventory, or
the `axnoded` to `imagemgr` integration.

## Implementation Map

```mermaid
flowchart TB
    Axnoded["runtime/axnoded"] -->|"image rootfs API over Unix socket"| Worker["api.HttpWorker"]

    Worker --> Store["internal/mountstore"]
    Worker --> OCI["oci.Manager"]
    Worker --> Nydus["nydus.RegistryClient"]
    Worker --> IFSMgr["imagefsd.Manager"]
    Worker --> OSSLoop["ossloop.Manager"]

    IFSMgr --> IFSD["imagefsd daemon process"]
    IFSD --> RawFuse["raw image FUSE mount"]
    IFSD --> NydusFuse["Nydus RAFS FUSE mount"]
    OSSLoop --> OSSRootfs["directory rootfs from raw image"]
    OCI --> OCIRootfs["OCI extract + readonly overlay"]

    Worker --> Inventory["GET /inventory"]
```

## Mount Families

| Entry point | Primary owner | Result |
| --- | --- | --- |
| `POST /oss_mount` | `api` + `imagefsd` + `ossloop` | Remote raw image mounted by `imagefsd`, then loop-mounted as a directory rootfs |
| `POST /nydus_mount` | `api` + `nydus` + `imagefsd` | Registry bootstrap mounted as a Nydus RAFS rootfs |
| `POST /oci_mount` | `api` + `oci`, optionally `nydus` + `imagefsd` | Imported or pulled OCI image exposed as a readonly overlay, unless Nydus routing succeeds |
| `POST /oci_import` | `api` + `oci` | Local Docker archive imported into the node-local OCI cache |

`api.HttpWorker` is the routing point. Keep request validation, daemon ID
generation, mount record writes, and route selection there instead of moving
mount policy into `axnoded` or lower-level packages.

## OCI Mount Routing

```mermaid
sequenceDiagram
    participant Client as axnoded or operator
    participant API as api.HttpWorker
    participant Store as mountstore
    participant Nydus as nydus.RegistryClient
    participant IFSD as imagefsd.Manager
    participant OCI as oci.Manager

    Client->>API: POST /oci_mount {image_url,lease_id,owner}
    API->>OCI: ResolveImportedImageCacheKey(image_url)
    alt image was imported through /oci_import
        API->>OCI: MountImageWithContextAndAuthKey(cache_key)
        API->>Store: Record OCI mount
    else no imported archive
        alt registry client configured and no inline Docker auth
            API->>Nydus: Detect bootstrap image
            alt Nydus image detected
                API->>IFSD: Create and mount Nydus daemon
                API->>Store: Record Nydus mount
            else not a Nydus image
                API->>OCI: Pull, extract, and overlay mount
                API->>Store: Record OCI mount
            end
        else no Nydus route for this request
            API->>OCI: Pull, extract, and overlay mount
            API->>Store: Record OCI mount
        end
    end
    API-->>Client: mount_path + immutable_mount + lease identity
```

Important routing rules:

- Imported images skip Nydus detection and use the local OCI cache.
- Request-scoped Docker config JSON applies only to the OCI pull path.
- If an image is detected as Nydus but the Nydus mount fails, the API returns
  that error instead of silently falling back to OCI.
- `POST /oci_umount` releases one durable lease. The final lease routes resource
  cleanup through `oci.Manager` or `imagefsd.Manager`; failed cleanup remains
  durable for reconciliation.

## OSS Raw Image Flow

```mermaid
sequenceDiagram
    participant Client as axnoded or operator
    participant API as api.HttpWorker
    participant IFSD as imagefsd.Manager
    participant Daemon as imagefsd daemon
    participant OSSLoop as ossloop.Manager

    Client->>API: POST /oss_mount {endpoint,bucket,object,lease_id,owner}
    API->>IFSD: CreateDaemon(raw image options)
    IFSD-->>API: daemon mountpoint + raw image name
    API->>Daemon: Mount raw object with imagefsd
    API->>OSSLoop: Mount ext4 raw image as directory rootfs
    API-->>Client: mount_path + immutable_mount + lease identity
```

The OSS flow is intentionally two-stage. `imagefsd` exposes the raw remote
image as a file, and `ossloop` converts that file into the directory rootfs
that `axnoded` can use. Preserve that split unless the rootfs model is being
redesigned deliberately.

OCI, Nydus, and OSS resources share the mountstore lease contract. Their
resource implementations remain owned by `oci`, `imagefsd`, and `ossloop`.
Each owner returns one bounded flat immutable-mount descriptor; axnoded
projection consumes that descriptor and must not reverse-engineer these
implementations. Source health and identity stay with imagemgr lease
reconciliation, while projection owns only its host OverlayFS and writable
artifacts.
Callers recover ownership by submitting their complete desired lease set to
`POST /reconcile_mount_leases`; reconciliation is scoped to that owner.

## Inventory And Cleanup

`GET /inventory` is the read-only node image summary. It combines:

- mount records from `internal/mountstore`
- imported and mounted OCI state from `oci.Manager`
- live daemon state from `imagefsd.Manager`
- retained ChunkDB and locality summaries from `imagefsd`

`POST /cleanup_daemon` removes a specific `imagefsd` daemon by daemon ID. Prefer
normal unmount endpoints for mounted rootfs cleanup because they also release
the owning mount records and route through the correct package owner.

## Change Routing

- API request or response changes: update `api/types.go`, tests, and
  `README.md` examples together.
- `imagefsd` daemon invocation changes: update this document,
  `runtime/imagefsd/docs/architecture.md`, and launch validation.
- Socket, `.dev/`, or rootfs routing changes: update `.x/runtime-stack.md` and
  the affected subsystem docs together.
