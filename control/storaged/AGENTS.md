# AGENTS.md

## Purpose

This is the local agent contract for `control/storaged`.

Read the [Storage Control Plane README](README.md) and [Storage Design](docs/design.md) before
changing storage protocol, storage domain rules, or service wiring.

## Architecture Rules

- Keep public storage contracts in `sdk/proto/axern/control/storage/v1`.
- Keep private storage coordination contracts in
  `sdk/proto/axern/private/storage/v1`.
- Keep pure storage policy, validation, and state transitions in
  `internal/kernel/storage`.
- Keep use-case orchestration in `internal/application/storage`.
- Keep test stores and fakes under `internal/storetest`.
- Do not add Kubernetes, cloud-provider, CSI, or Postgres implementation detail
  to the kernel package.
- Do not make `storaged` a node runtime path. `controld` remains the V1
  control-plane-to-node lifecycle transport owner.

## Validation

For Go changes, run:

```bash
make -C control/storaged test
make -C control/storaged vet
test -z "$(gofmt -l control/storaged)"
```

For proto changes, run:

```bash
make -C sdk/proto generate-go
make -C sdk/proto lint
make -C sdk/proto breaking
```
