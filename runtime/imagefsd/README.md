# imagefsd

`imagefsd` is a read-only FUSE filesystem for raw disk images and Nydus RAFS images. The implementation combines:

- on-demand reads from local or remote storage
- sparse local cache files with bitmap sidecars
- optional chunk-level deduplication backed by LMDB
- an optional local/peer chunk server for chunk sharing
- optional OTLP metrics export

## Related Docs

- Cross-component runtime log meanings: [Runtime Logs](../../docs/operations/runtime-logs.md)
- Implementation map, mount flow, and chunk-server boundaries: [Architecture](docs/architecture.md)

## Supported Behavior

- `mount` is implemented on Linux only. On non-Linux targets, mounting returns an unsupported-operation error.
- `--src local` mounts a single raw file from the local filesystem.
- `--src oss` mounts a single raw file from a remote backend created from a Nydus `BackendConfigV2` JSON file. Despite the flag name, this path is not OSS-only. With the enabled build features it can use OSS, S3, registry, and HTTP-proxy backends exposed by `nydus-storage`.
- `--src nydus` mounts a Nydus RAFS filesystem from a bootstrap plus backend config.
- Optional dedup uses:
  - `ChunkDB`: global content-addressed chunk storage
  - `IndexDB`: per-image offset-to-checksum metadata
- `serve-chunk` exposes a local `ChunkDB` over both TCP and a Unix socket, and can cooperate with peers plus an optional Redis-backed chunk ownership index.
- `gc-chunk`, `stats-chunk`, and `stats-locality` operate on node-local chunk state.

## Important Behavior And Constraints

- The filesystem is read-only.
- Raw-image mounts expose exactly one file at the mountpoint root. That file is named by `--name`.
- For raw-image mounts, `--name` must be a single path component. The implementation reuses it as both the mounted filename and the raw-image backend identifier.
- For raw-image mounts, `--name` is also used as the dedup metadata identity. Reuse the same `--name` only for the same image namespace/content lineage when sharing an `image_meta_dir`.
- `--chunk-db-dir` and `--image-meta-dir` must be configured together or both omitted.
- The cache layer creates a sidecar bitmap file next to each cache file: `<cache path>.bitmap`.
- The cache layer fills a chunk across normal partial backend reads. End-of-file before the chunk is complete is an error and never marks the sparse-cache chunk valid.
- `--src local` opens the source file read-write. When dedup is enabled, chunks that have been persisted to `ChunkDB` may be hole-punched from that file. Use a dedicated writable cache copy if you need to preserve the original file byte-for-byte.
- In Nydus mode, `--name` is still required by the CLI parser, but it is not used by the mounted filesystem.
- In Nydus mode, `--cache-dir` is optional. If set, blob payloads are cached on disk and wrapped in the same cache/dedup stack. If omitted, blob ranges are fetched directly from the backend, while decompressed chunks can still be served from and stored into `ChunkDB` when dedup is enabled.
- `--nydus-readahead-workers` starts a bounded worker pool for demand-triggered
  Nydus cache readahead. `--nydus-readahead-window-bytes` limits each hint.
  Readahead requires `--cache-dir`; zero workers disables it.
- `--nydus-decoded-cache-bytes` bounds the per-mount memory used to coalesce
  concurrent decodes and briefly reuse decompressed Nydus chunks. The default
  is 8 MiB; zero retains no chunks after an in-flight load completes. When
  ChunkDB is enabled, successful asynchronous persistence immediately releases
  the corresponding decoded entry; failed persistence remains retryable.
- The Nydus read path does not support encrypted chunks or batched chunks.
- `gc-chunk --dry-run` only skips deletion. It does not print a deletion plan.

## Build

Prerequisites:

- Rust 2021 toolchain
- Linux with FUSE support for `mount`
- writable directories for cache files, bitmap sidecars, LMDB data, and Unix sockets
- optional Redis if you want dynamic peer discovery or a Redis-backed chunk index

Build the main binary:

```bash
cargo build -p imagefsd
```

Run the CLI help:

```bash
cargo run -p imagefsd -- --help
```

## Debug In Devbox

`imagefsd` is expected to be built and debugged from the repository-root Linux workspace. Use the root devbox for the local privileged `arm64` path.

```bash
make devbox-up

# Inside the Linux workspace
make node-dev-prepare
make imagefsd-build
make imagefsd-dev-serve-chunk
```

The repository writes chunk-server state to `.dev/imagefsd/chunkdb` and exposes the Unix socket at `.dev/run/imagefsd-chunk.sock`.

VS Code launch support is available through the repository-level [`../../.vscode/launch.json`](../../.vscode/launch.json) entry `Imagefsd: Debug chunk server`. That launch uses `cppdbg + gdb` against `${workspaceFolder}/target/debug/imagefsd`, so the Linux workspace must provide `gdb`.

## CLI Overview

The main binary exposes five subcommands:

| Command | Purpose |
| --- | --- |
| `mount` | Mount a raw image or Nydus filesystem |
| `serve-chunk` | Serve local chunks over TCP and Unix socket, optionally with peer coordination |
| `gc-chunk` | Garbage-collect old/LRU chunks from `ChunkDB` |
| `stats-chunk` | Print lightweight JSON stats for `ChunkDB` |
| `stats-locality` | Query Unix-socket locality stats used by `imagemgr` for chunk/peer heat summaries |

Global flags:

- `-d`, `--verbose`: enable debug logging

Common operational flags:

- `--daemon`: daemonize the process
- `--pid-file`: PID file path to use with `--daemon`
- `--log-file`: log file path; if set, logs go through the rotating file writer
- `--node-id`: stable control-plane node identity for mount telemetry
- `--otel-endpoint`: override the standard `OTEL_EXPORTER_OTLP_ENDPOINT` used
  for OTLP metrics export. The Nydus mount path emits startup-critical phase
  histograms under `imagefsd.mount.startup_phase_duration_seconds` with
  `canonical_phase=rootfs_prepare`.

The sparse cache exports `imagefsd.cache.backend_fetch_duration_ms` with
`path=foreground|readahead` and bounded result values including `ok`,
`io_error`, and `short_read`.
`imagefsd.cache.inflight_wait_duration_ms` records time spent waiting for the
leader of a same-chunk fetch. These attribution histograms carry the bounded
`node_id` label; process identity comes from the OTLP resource. Image refs,
blob IDs, cache paths, and other workload identities are not metric labels.

## Mount Modes

### `--src local`

Mount an existing local raw file as a single file in the mountpoint.

Required arguments:

- `--name`: filename exposed inside the mountpoint and dedup metadata key
- `--mountpoint`: FUSE mountpoint
- `--cache-file`: path to the existing local raw file

Example:

```bash
mkdir -p /mnt/raw

cargo run -p imagefsd -- mount \
  --src local \
  --name disk.raw \
  --cache-file /data/disk.raw \
  --mountpoint /mnt/raw
```

### `--src oss`

Mount a remote raw file as a single file in the mountpoint. The implementation uses `GeneralBackend`, which loads a Nydus `BackendConfigV2` JSON file.

Required arguments:

- `--name`: remote object/blob identifier and mounted filename; it must still be valid as a single filename inside the mountpoint
- `--mountpoint`: FUSE mountpoint
- `--cfg`: backend config JSON
- `--cache-file`: writable local cache file path

Example:

```bash
mkdir -p /mnt/raw /var/cache/distill

cargo run -p imagefsd -- mount \
  --src oss \
  --name rootfs.raw \
  --cfg /etc/distill/backend.json \
  --cache-file /var/cache/distill/rootfs.raw \
  --mountpoint /mnt/raw
```

### `--src nydus`

Mount a Nydus RAFS filesystem from a bootstrap file plus backend config.

Required arguments:

- `--name`: required by the CLI, but ignored by the Nydus mount logic
- `--mountpoint`: FUSE mountpoint
- `--bootstrap`: Nydus bootstrap path
- `--cfg`: backend config JSON

Optional but common:

- `--cache-dir`: writable directory for blob cache files
- `--nydus-readahead-workers`: bounded concurrent readahead workers
- `--nydus-readahead-window-bytes`: maximum range scheduled after a foreground read
- `--nydus-decoded-cache-bytes`: per-mount decoded chunk cache limit

Example:

```bash
mkdir -p /mnt/nydus /var/cache/distill/blobs

cargo run -p imagefsd -- mount \
  --src nydus \
  --name nydus-image \
  --bootstrap /images/bootstrap.rafs \
  --cfg /etc/distill/backend.json \
  --cache-dir /var/cache/distill/blobs \
  --nydus-readahead-workers 2 \
  --nydus-readahead-window-bytes 33554432 \
  --nydus-decoded-cache-bytes 8388608 \
  --mountpoint /mnt/nydus
```

## Enabling Dedup

Dedup is optional for both raw and Nydus mounts. To enable it, configure both:

- `--chunk-db-dir`
- `--image-meta-dir`

When enabled:

- `ChunkDB` stores chunk payloads globally by checksum
- `IndexDB` stores per-image range metadata
- raw-image reads can fall back from cache/backend to chunk reuse
- Nydus reads can serve decompressed chunks from `ChunkDB` and asynchronously persist newly read chunks into it

Example with dedup enabled:

```bash
mkdir -p /mnt/raw /var/cache/distill /var/lib/distill/chunkdb /var/lib/distill/imagedb

cargo run -p imagefsd -- mount \
  --src oss \
  --name rootfs.raw \
  --cfg /etc/distill/backend.json \
  --cache-file /var/cache/distill/rootfs.raw \
  --chunk-db-dir /var/lib/distill/chunkdb \
  --image-meta-dir /var/lib/distill/imagedb \
  --mountpoint /mnt/raw
```

## Local And Peer Chunk Serving

`serve-chunk` serves a local `ChunkDB` in two ways at the same time:

- TCP on `0.0.0.0:<listen-port>`
- Unix socket for same-host clients

If `--chunk-server-sock` is omitted, the Unix socket defaults to:

```text
<chunk_db_dir>/chunkserver.sock
```

Mount processes that use the same `chunk_db_dir` automatically default to that socket path too.

### Peer configuration

There are two peer-discovery modes:

- static peers via `--peer-addrs host:port,host:port`
- Redis discovery via `--peer-discovery redis://...`

If `--peer-discovery` is set, the code expects it to be a Redis URL. Other discovery schemes are rejected.

`--chunk-index-url` is separate from discovery. If configured, it enables a Redis-backed chunk ownership index used by the peer client/chunk server.

Example:

```bash
mkdir -p /var/lib/distill/chunkdb

cargo run -p imagefsd -- serve-chunk \
  --chunk-db-dir /var/lib/distill/chunkdb \
  --listen-port 9876 \
  --advertise-addr 10.0.0.11:9876 \
  --peer-discovery redis://10.0.0.20:6379/0 \
  --chunk-index-url redis://10.0.0.20:6379/1
```

Useful options:

- `--node-id`: explicit node identity; otherwise defaults to `$HOSTNAME` or `distill-fs-<pid>`
- `--chunk-index-ttl`: chunk ownership TTL in seconds, default `43200`
- `--tokio-worker-threads`: async runtime worker count, default `8`

## Garbage Collection

`gc-chunk` works on `ChunkDB` only.

GC behavior:

- delete chunks whose access time is older than 24 hours
- if LMDB usage is still above 90%, delete least-recently-used chunks in batches until usage falls to 80% or no more candidates exist

Run GC:

```bash
cargo run -p imagefsd -- gc-chunk \
  --chunk-db-dir /var/lib/distill/chunkdb
```

Use a non-default local chunk server socket when needed:

```bash
cargo run -p imagefsd -- gc-chunk \
  --chunk-db-dir /var/lib/distill/chunkdb \
  --chunk-server-sock /run/distill/custom-chunkserver.sock
```

Skip deletions:

```bash
cargo run -p imagefsd -- gc-chunk \
  --chunk-db-dir /var/lib/distill/chunkdb \
  --dry-run
```

## ChunkDB Stats

`stats-chunk` prints lightweight JSON without a full chunk scan.

Example:

```bash
cargo run -p imagefsd -- stats-chunk \
  --chunk-db-dir /var/lib/distill/chunkdb
```

Representative output:

```json
{
  "storage": {
    "total_size_bytes": 107374182400,
    "used_size_bytes": 52428800,
    "free_size_bytes": 107321753600,
    "usage_percent": "0.05"
  },
  "chunks": {
    "total_count": 1234
  },
  "access_time": {
    "oldest_epoch_secs": 1707500000,
    "newest_epoch_secs": 1707600000
  }
}
```

## Locality Stats

`stats-locality` queries the local Unix chunk socket and returns the read-only locality summary used by `imagemgr /inventory`.

Example:

```bash
cargo run -p imagefsd -- stats-locality \
  --chunk-db-dir /var/lib/distill/chunkdb \
  --chunk-server-sock /var/lib/distill/chunkdb/chunk.sock
```

If the database has no access records yet, the access-time fields may be `null`.

## Read Path Summary

### Raw images

1. Read through the FUSE file exposed at the mountpoint root.
2. If dedup metadata exists, try `ChunkDB` first for covered ranges.
3. If data is not available in `ChunkDB`, read from the cache/backend.
4. Queue background dedup work for touched 4 MiB chunks.
5. Once a chunk has been persisted to `ChunkDB`, the cache layer can invalidate that chunk and reclaim space by hole-punching the local cache file.

### Nydus images

1. Resolve inodes from the RAFS bootstrap.
2. For each file chunk, compute the expected checksum from RAFS metadata.
3. Try `ChunkDB` first.
4. On a miss, fetch the compressed chunk range from the backend or blob cache, then decompress if necessary.
5. Return the requested slice to FUSE and enqueue the full uncompressed chunk for background storage in `ChunkDB`.

The fixed chunk size used by the dedup/cache stack is 4 MiB.

## Backend Configuration

This project does not define its own remote-backend JSON schema. The `--cfg` file for remote raw and Nydus modes is deserialized directly as Nydus `BackendConfigV2`.

In practice this means:

- reuse the backend JSON format you already use with Nydus tooling
- the accepted fields depend on the backend type selected in that JSON
- the available backend types depend on which `nydus-storage` features were compiled into this binary

## Development

Common commands:

```bash
cargo build -p imagefsd
cargo run -p imagefsd -- --help
cargo test -p imagefsd -- --test-threads=1
```

Redis integration tests are behind a feature flag:

```bash
cargo test --features redis-integration-tests redis_ -- --test-threads=1
```

Override the Redis target if needed:

```bash
DISTILL_FS_TEST_REDIS_URL=redis://host:6379/15 \
  cargo test --features redis-integration-tests redis_ -- --test-threads=1
```

Additional benchmark binaries:

```bash
cargo run --bin chunkdb_bench -- --help
cargo run --bin peer_bench -- --help
```

## License

Apache-2.0
