# AGENTS.md

## Purpose

Local contract for the Axern Python SDK workspace. Follow the root
[`../../AGENTS.md`](../../AGENTS.md) first, then apply this file under
`sdk/python`.

## Workspace Boundaries

- Keep importable code under [`src`](src), generated protobuf modules under
  [`src/axern`](src/axern), and user-facing SDK code under
  [`src/axern_sdk`](src/axern_sdk).
- Keep tests under [`tests`](tests).
- Do not add product apps, demos, services, or platform entrypoints here.

## API And Structure

- Treat `axern_sdk.__init__`, `client`, `catalog`, `sandbox`, and `tunnel` as
  public API boundaries; keep exports intentional and small.
- Do not add compatibility aliases for flawed early API shapes unless a concrete
  external contract requires them.
- Prefer explicit source models over inferred defaults. Environment and sandbox
  callers should provide exactly one source: `template_id`, `image`, or
  `environment_id`.
- Keep files cohesive by responsibility. Do not grow orchestration files into
  mixed client, lifecycle, transport, and model implementations.
- Put private shared helpers in `axern_sdk._internal` when they are not part of
  the SDK surface.
- Do not hand-edit generated `*_pb2.py` or `*_pb2_grpc.py` files.

## Proto Generation

- Regenerate Python protobuf modules with
  [`scripts/generate_proto.sh`](scripts/generate_proto.sh).
- When adding proto dependencies, update the generation script, required
  generated `__init__.py` files, [`pyproject.toml`](pyproject.toml), and
  [`../../uv.lock`](../../uv.lock) together.

## Sandbox And Tunnel Rules

- `Sandbox` owns SDK-created environment, service, tunnel session, connector,
  renewal, and cleanup lifecycle.
- Long-lived tunnel support must renew finite tunnel TTLs automatically.
- Cleanup is best-effort and must not mask startup failures or wait too long
  after an earlier cleanup step failed.
- Tunnel framing, renewal, cleanup, or readiness changes need focused unit tests
  and compose SDK e2e validation.

## Validation

- Python SDK code:

  ```bash
  make test-py
  make lint-py
  ```

- Package metadata, generated proto, dependency, or distribution changes:

  ```bash
  uv build sdk/python
  ```

- `Sandbox`, tunnel, cleanup, service lifecycle, or compose workflow changes:

  ```bash
  make local-compose-python-sdk-e2e
  ```

- This file or repo-local documentation:

  ```bash
  make agent-doc-check
  ```
