# Tracing

`imagemgr` has timing-oriented OpenTelemetry instrumentation behind the `-enable_tracing` flag.

## What The Flag Enables

When `-enable_tracing` is set, `internal/app` installs a tracer provider with `service.name=imagemgr` and passes tracing context through the daemon.

The binary uses that provider for:

- `nydus/`: registry fetch and bootstrap extraction timing
- `api/`: mount and unmount request timing
- `imagefsd/`: launched daemon mount and unmount timing

The same instrumentation also writes stage timing summaries to the standard `imagemgr` log stream.

## Run With Tracing

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr \
  -imagefsd_bin /usr/local/bin/imagefsd \
  -oss_template ./configs/oss_backend.json.example \
  -nydus_template ./configs/nydus_registry.json.example \
  -oss_auths_path ./oss_auths.json.example \
  -registry_auths_path ./registry_auths.json.example \
  -http_sock /tmp/imagemgr.sock \
  -enable_tracing
```

Logs are written under `<root>/logs/imagemgr.log`.

## Example Timing Summaries

Representative log lines look like this:

```text
INFO API trace operation=api.MountNydus identifier=abc123 total=5.2s, validate_request=1ms, check_existing_daemon[rootfs_prepare]=5ms, prepare_options[rootfs_prepare]=2ms, create_daemon[rootfs_prepare]=3.8s, get_daemon[rootfs_prepare]=1ms, daemon_mount[rootfs_prepare]=1.4s
INFO Nydus trace operation=nydus.FetchAndExtractBootstrap image_url=docker.io/library/alpine:latest total=3.4s, fetch_image[rootfs_prepare]=2.8s, check_nydus_format[rootfs_prepare]=10ms, extract_bootstrap[rootfs_prepare]=646ms
INFO imagefsd trace operation=daemon.Mount daemon=abc123 total=1.2s, clean_mount_point[rootfs_prepare]=10ms, apply_config[rootfs_prepare]=5ms, start_daemon_process[rootfs_prepare]=800ms, save_metadata[rootfs_prepare]=2ms, wait_mount_ready[rootfs_prepare]=417ms
```

The exact durations vary, but the operation names, identifiers, and stage names come directly from code.

## Timed Operation Layers

Three timed-operation helpers are used:

- `Nydus trace` Registry fetch, Nydus detection, and bootstrap extraction
- `API trace` Request parsing, daemon lookup, daemon creation, mount routing, and loop mount orchestration
- `imagefsd trace` `imagefsd` daemon startup, config write, mount readiness, and unmount flow

## Stage Mapping

The most useful stage groupings when reading logs are:

- Nydus registry path: `fetch_image`, `check_nydus_format`, `extract_bootstrap`
- OSS API mount path: `parse_request`, `validate_dependencies`, `prepare_options`, `create_daemon`, `get_daemon`, `daemon_mount`, `loop_mount`
- Nydus API mount path: `validate_request`, `check_existing_daemon`, `mount_existing_daemon`, `prepare_options`, `create_daemon`, `get_daemon`, `daemon_mount`
- imagefsd daemon startup path: `clean_mount_point`, `apply_config`, `fetch_bootstrap`, `wait_daemon_running`, `start_daemon_process`, `save_metadata`, `wait_mount_ready`
- imagefsd daemon teardown path: `signal_stop`, `send_sigterm`, `wait_graceful_exit`, `send_sigkill`, `wait_forced_exit`, `clean_mount_point`

Stage names are emitted exactly as recorded in code, for example:

- `parse_request`
- `validate_dependencies`
- `check_existing_daemon`
- `create_daemon`
- `daemon_mount`
- `loop_mount`
- `fetch_image`
- `check_nydus_format`
- `extract_bootstrap`
- `start_daemon_process`
- `wait_mount_ready`

Stages that contribute to rootfs preparation are automatically tagged with the canonical phase `rootfs_prepare`.

## Export Behavior

The binary uses the shared Axern OpenTelemetry setup. Enable export with `AXERN_OTEL_ENABLED=true` and the standard OTLP environment variables; local compose and kind route signals through the bundled collector into Grafana LGTM.

Timed operations export two histogram levels:

- `axern.imagemgr_timed_operation_duration_seconds` records total operation latency by `axern_operation` and `axern_result`.
- `axern.imagemgr_timed_operation_stage_duration_seconds` records per-stage latency by `axern_operation`, `axern_stage`, canonical `axern_phase`, and `axern_result`.

The stage metric intentionally does not label image ref, cache key, path, mountpoint, or daemon ID. Use traces and logs for single-image debugging; use the metric to compare stable stage costs such as registry fetch, layer extraction, daemon creation, and mount readiness.
