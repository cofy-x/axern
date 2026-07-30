---
title: Compose Quickstart
description: Start the complete Axern stack and run a sandbox in ten minutes.
---

The supported release path starts PostgreSQL, MinIO, the control plane,
gateway, node services, and a smoke workload from published artifacts.

## Start Axern

```bash
git clone https://github.com/cofy-x/axern.git
cd axern
make quickstart
```

`make quickstart` waits for readiness and runs a core smoke through the public
gateway. The generated CLI and context stay under `deploy/local/state/`.

## Run a sandbox

```bash
AXERN_CLI=deploy/local/state/releases/v$(cat VERSION)/axern

"${AXERN_CLI}" context current
"${AXERN_CLI}" run create \
  --image-ref docker.io/library/python:3.12-slim \
  --runtime-class runsc \
  --argv python \
  --argv -c \
  --argv 'print("hello from Axern")' \
  --wait
```

Use `--output json` for automation. A normally terminated `run create --wait`
returns the workload's exit code.

## Inspect and clean up

```bash
make local-compose-status
make local-compose-purge
```

Purge removes the Compose containers, generated state, and local development
database. It does not delete unrelated Docker resources.

## Develop from source

The source path remains separate and builds the current checkout into local
`:dev` images:

```bash
make quickstart-source
```

Both paths exercise the same Compose and public gateway contract.
