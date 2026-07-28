# AGENTS.md

## Purpose

This is the agent contract for `runtime/volumed`.

Read the [Volume Runtime README](README.md) before editing. Keep this file focused on
runtime-node volume ownership, package boundaries, and validation.

## Architecture Rules

- Keep [`cmd/volumed`](cmd/volumed) as a thin daemon entrypoint.
- Keep process wiring, flags, Unix-socket serving, and shutdown orchestration in
  [`internal/app`](internal/app).
- Keep gRPC request validation and proto mapping in [`internal/api`](internal/api).
- Keep node physical volume publish, unpublish, provider selection, runtime
  compatibility checks, and persisted publish state in
  [`internal/storage`](internal/storage).
- `volumed` owns node-local physical storage behavior. It must not call
  `controld`, `control/storaged`, or make allocation placement decisions.
- `axnoded` remains the allocation lifecycle orchestrator. It calls `volumed`
  with resolved node volume specs and turns returned published volumes into OCI
  mounts.
- Provider implementations must validate host path, target path, mount options,
  and backend-specific parameters before returning a publish result.

## Sync Points

- API changes: update `sdk/proto`, regenerate Go stubs, and update
  `runtime/axnoded` client code and tests together.
- Socket, flag, or deployment changes: update `README.md`, root docs,
  `.vscode`, `deploy/images/lib/node-all-in-one-entrypoint.sh`, local
  kind/compose wiring, and Helm node templates together.
- Provider semantics that affect service volume behavior should also update
  `control/storaged/docs/design.md` and `runtime/axnoded/README.md` when the
  runtime boundary changes.

## Validation

For generic Go changes:

```bash
make test
make vet
```

For cross-runtime changes, also run from the repository root:

```bash
make -C runtime/axnoded test-host
make local-compose-service-volume-smoke
make kind-service-volume-smoke
```
