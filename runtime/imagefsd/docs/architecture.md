# Imagefsd Architecture

`imagefsd` is the read-only image data plane used by `imagemgr` for raw-image
and Nydus rootfs flows. It exposes FUSE mounts for images and can share chunks
through a node-local or peer-aware chunk server.

Use this document when changing mount internals, dedup behavior, chunk serving,
or the CLI contract consumed by `runtime/imagemgr`.

## Implementation Map

```mermaid
flowchart TB
    Main["src/main.rs"] --> CLI["src/cli.rs"]
    CLI --> MountCmd["cli::mount"]
    CLI --> ChunkCmd["cli::chunk"]

    MountCmd --> FS["src/fs.rs mount_fs"]
    MountCmd --> Raw["image::raw::RawImage"]
    MountCmd --> Nydus["image::nydus::NydusImage"]

    Raw --> GeneralBackend["backend::general"]
    Raw --> Cache["backend::cache"]
    Nydus --> NydusBackend["nydus_storage backend"]
    Nydus --> Cache
    NydusBackend --> Dragonfly["Dragonfly Seed Client Service"]
    Dragonfly --> Scheduler["Dragonfly scheduler and peers"]

    Raw --> Dedup["backend::dedup"]
    Nydus --> Dedup
    Dedup --> ChunkDB["backend::chunkdb::ChunkDB"]
    Dedup --> IndexDB["per-image IndexDB"]

    ChunkCmd --> ChunkServer["backend::peer::ChunkServer"]
    ChunkServer --> ChunkDB
    ChunkServer --> Peer["peer discovery and optional Redis index"]
    MountCmd --> LocalChunk["LocalChunkClient"]
    LocalChunk --> ChunkServer
```

## Mount Flow

```mermaid
sequenceDiagram
    participant Caller as imagemgr or operator
    participant CLI as mount command
    participant Backend as backend/cache layer
    participant Dedup as ChunkDB + IndexDB
    participant Image as RawImage or NydusImage
    participant FUSE as mount_fs

    Caller->>CLI: imagefsd mount ...
    CLI->>Backend: Open source and cache
    opt --chunk-db-dir and --image-meta-dir
        CLI->>Dedup: Open chunk database and image metadata
    end
    alt --src local or --src oss
        CLI->>Image: Build RawImage
    else --src nydus
        CLI->>Image: Build NydusImage
    end
    CLI->>FUSE: Mount read-only filesystem
    FUSE-->>Caller: mountpoint ready
```

Mount ownership stays split by source type:

- `--src local` wraps an existing local raw file.
- `--src oss` uses `GeneralBackend` with a Nydus backend config, so the path is
  not OSS-only even though the flag name is historical.
- `--src nydus` builds a RAFS filesystem from a bootstrap and backend config.

## Nydus Data Plane

Nydus metadata and blob data have separate ownership. `imagemgr` resolves the
image manifest and extracts the RAFS bootstrap. The mounted `imagefsd` process
owns compressed blob reads and the node-local sparse cache.

```mermaid
sequenceDiagram
    participant Imagemgr
    participant Imagefsd
    participant Proxy as Dragonfly Seed Client Service
    participant Peers as Dragonfly scheduler and peers
    participant Cache as Sparse blob cache
    participant Decoded as Decoded chunk cache
    participant FUSE

    Imagemgr->>Imagemgr: Resolve manifest and bootstrap
    Imagemgr->>Imagefsd: Start mount with bootstrap and backend config
    Imagefsd->>FUSE: Publish RAFS mount
    FUSE->>Cache: Read requested chunk
    Cache->>Proxy: Fetch foreground range on cache miss
    Proxy->>Peers: Prefetch and distribute the blob
    Proxy-->>Cache: Return requested bytes
    Cache-->>Decoded: Decode chunk once
    Decoded-->>FUSE: Return requested range
```

- `--nydus-readahead-workers` bounds demand-triggered background range reads;
  `--nydus-readahead-window-bytes` caps each hint. Zero workers keeps fully lazy
  imagefsd behavior and is the production default with Dragonfly prefetch.
- Mounting does not queue whole blobs. A successful foreground miss schedules
  only the following bounded window, starting at the next cache-chunk boundary.
- Background remote I/O does not own the foreground in-flight slot. The paths
  only contend during the short final cache commit, so startup-critical reads
  cannot wait behind a long readahead request.
- The byte-bounded decoded chunk cache coalesces concurrent misses by checksum
  and retains only a small per-mount working set. `ChunkDB` remains the durable,
  cross-mount chunk cache; successful persistence releases the decoded entry,
  while queue or storage failures remain retryable on later reads. Decoded
  memory is not an image cache replacement.
- Registry authentication is resolved by the Nydus registry backend and
  forwarded through the Dragonfly Seed Client Service HTTP proxy.
- Direct registry fallback is a reliability path, not a successful Dragonfly
  performance sample. Production validation must report proxy errors and
  fallback separately.
- Mount processes use `OTEL_EXPORTER_OTLP_ENDPOINT` by default. An explicit
  `--otel-endpoint` overrides the environment.

## Dedup And Chunk Reuse

- Dedup is enabled only when `--chunk-db-dir` and `--image-meta-dir` are both
  configured.
- `ChunkDB` is global content-addressed chunk storage.
- `IndexDB` is per-image offset-to-checksum metadata.
- Raw-image dedup identity depends on `--name`; changing that meaning is a
  compatibility change for `imagemgr`.
- Nydus mode requires `--name` at the CLI level, but the filesystem does not use
  it.
- A local chunk client can reuse chunks from the node-local chunk server before
  falling back to the configured backend.

## Chunk Server Flow

```mermaid
sequenceDiagram
    participant Operator as imagemgr/dev workflow
    participant CLI as serve-chunk command
    participant DB as ChunkDB
    participant Runtime as PeerRuntime
    participant Server as ChunkServer
    participant Client as mount/gc/stats clients

    Operator->>CLI: imagefsd serve-chunk ...
    CLI->>Runtime: Configure node id, listen address, discovery
    CLI->>DB: Open chunk database
    opt Redis chunk index configured
        CLI->>Runtime: Attach chunk ownership index
    end
    CLI->>Server: Serve TCP and Unix socket APIs
    Client->>Server: Query, store, reuse, GC, or inspect chunks
```

The operational chunk commands are:

- `serve-chunk`: expose local chunks over TCP and a Unix socket.
- `gc-chunk`: garbage-collect old or LRU chunks through the local socket path.
- `stats-chunk`: read lightweight `ChunkDB` stats directly.
- `stats-locality`: query locality summaries through the Unix socket.

## Compatibility Boundaries

- The filesystem is read-only.
- Raw and Nydus image implementations stay separate under `src/image`.
- Backend, cache, dedup, chunk database, and peer logic stay under
  `src/backend`.
- CLI flags used by `imagemgr` are cross-component contracts.
- FUSE behavior must be validated on Linux; macOS checks can cover build and
  unit logic only.
