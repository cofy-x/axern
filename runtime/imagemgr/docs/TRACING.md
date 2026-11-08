# Tracing

`imagemgr` uses the shared Axern OpenTelemetry setup with `service.name=imagemgr`. Enable it with `-enable_tracing` or `AXERN_OTEL_ENABLED=true`; configure export with the standard OTLP environment variables. Local Compose and kind require an explicit operator-owned OTLP endpoint; they do not install telemetry backends.

## What The Flag Enables

HTTP operation spans cover OCI mount, unmount, import and inventory requests. `oci` adds timed operations for image mounting, unmounting and layer garbage collection. The same helpers write stage summaries to `<root>/logs/imagemgr.log`; API completion logs include operation, elapsed seconds and errors.

## Run With Tracing

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr \
  -http_sock /tmp/imagemgr.sock \
  -enable_tracing
```

Logs are written under `<root>/logs/imagemgr.log`.

## Operation Names

HTTP spans are `imagemgr.oci.mount`, `imagemgr.oci.unmount`, `imagemgr.oci.import` and `imagemgr.inventory`. Their API operation names are `oci_mount`, `oci_unmount`, `oci_import` and `inventory`. See `api/server.go` and `internal/observability/names.go` when changing this contract.

Timed OCI operations are `oci.MountImage`, `oci.UnmountImage` and `oci.GCLayers`. Successful summaries use the `OCI trace` prefix. Failures record the error and emit `timed operation failed`. Identifiers belong in traces and logs rather than metric labels.

## Stage Mapping

Mount stages include `validate_request`, `check_existing_mount`, `fetch_image`, `list_layers`, `build_chain_ids`, `extract_layers`, `prepare_lowerdirs`, `persist_mount_txn`, `overlay_mount` and `persist_mount_record`. Reuse paths emit `reuse_in_memory_mount` or `reuse_persisted_mount`. Unmount stages cover lookup, overlay unmount, record deletion and layer/chain reference decrements. Garbage collection records TTL and disk-pressure stages separately.

`internal/observability/timedtrace` maps mount-preparation stages to the canonical phase `rootfs_prepare`. Stage names and mappings are defined by the code, so compare the current `oci` callsites before changing dashboards or interpreting a missing stage.

## Export Behavior

HTTP requests export `axern.imagemgr_http_operation_total` and `axern.imagemgr_http_operation_duration_seconds`. Timed OCI operations export `axern.imagemgr_timed_operation_total` and two histogram levels:

- `axern.imagemgr_timed_operation_duration_seconds` records total operation latency with `axern.operation` and `axern.result` attributes.
- `axern.imagemgr_timed_operation_stage_duration_seconds` records per-stage latency with `axern.operation`, `axern.stage`, canonical `axern.phase` and `axern.result` attributes.

The stage metric does not label image ref, cache key, path or mountpoint. Use traces and logs for single-image debugging; use metrics to compare stable stage costs such as registry fetch, layer extraction and overlay mounting.
