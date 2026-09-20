# Python SDK Agent Contract

## Purpose

`sdk/python` owns the public Python SDK. Use its [README](README.md) for usage and commands.

## Ownership Boundaries

- Keep generated protobuf modules under `src/axern`, public SDK code under `src/axern_sdk`, private shared helpers under `axern_sdk._internal`, and tests under `tests`.
- Keep exports intentional and do not hand-edit generated `*_pb2.py` or `*_pb2_grpc.py` files.
- Model sources explicitly: Environment creation requires an OCI `image`; Sandbox creation accepts exactly one of `image` or `environment_id`.
- `Sandbox` is the SDK facade over an SDK-created Environment, Run, and current Allocation. Process, file, terminal, SSH, and Tunnel operations reject stale Allocation targets.
- Finite session renewal and cleanup cannot hide the originating error or extend Allocation authority.

## Validation

Run `make test-py`, `make lint-py`, and the package, generated-output, or integration checks selected by `make verify-changed`.
