# Python SDK Agent Contract

## Purpose

`sdk/python` owns the public Python SDK. Read the [Python SDK README](README.md) for usage and package commands.

## Ownership Boundaries

- Keep generated protobuf modules under `src/axern`, public SDK code under `src/axern_sdk`, private shared helpers under `axern_sdk._internal`, and tests under `tests`.
- Keep exports intentional and do not hand-edit generated `*_pb2.py` or `*_pb2_grpc.py` files.
- Model sources explicitly: Environment and Sandbox creation accepts exactly one of `template_id`, `image`, or `environment_id`.
- `Sandbox` is the SDK facade over an SDK-created Environment, Run, and current Allocation. Process, file, terminal, SSH, and Tunnel operations reject stale Allocation targets.
- Tunnel sessions use finite TTLs with bounded renewal. Cleanup is best-effort and must not mask the originating failure.
- Keep lifecycle, transport, models, and client responsibilities in focused modules rather than accumulating them in orchestration files.

## Validation

- Run `make test-py` and `make lint-py` for SDK changes.
- Regenerate protos with `sdk/python/scripts/generate_proto.sh` and run repository generated-output checks when protobuf contracts change.
- Run `uv build sdk/python` for package or dependency changes and the relevant Compose SDK truth selected by `make verify-changed` for lifecycle or Tunnel behavior.
