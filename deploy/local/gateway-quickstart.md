# Gateway Quickstart

This page keeps copyable local commands for checking `gatewayd` in Compose and
kind. The retained data-plane paths are allocation terminal/SSH, Tunnel,
artifact transfer, and sandbox operations. Gatewayd does not expose a Service
HTTP route or `/svc` compatibility path.

## Local Endpoints

| Environment | HTTP gateway | SSH |
| --- | --- | --- |
| compose | `http://127.0.0.1:25080` | `127.0.0.1:25022` |
| kind | `http://127.0.0.1:25082` | `127.0.0.1:25023` |

`axern ssh` reads the SSH target and generated client key from the current
Axern context. Use `axern ctx list` before testing if Compose and kind are both
running.

## Compose

Start or refresh Compose, select its context, and create a long-lived Run:

```bash
make local-images-build
make local-compose-up
axern ctx use compose

axern run --detach python:3.12-slim -- \
  python -c 'import time; time.sleep(3600)'
```

Read the returned Run with `axern run get <run-id> -o json` and copy its current
`allocation_id`. The Allocation is the explicit identity for the retained
gateway access paths:

```bash
axern ssh <allocation-id> -- uname -a
axern tunnel doctor --allocation-id <allocation-id> \
  --local 127.0.0.1:8080
```

Use `axern tunnel open` for a foreground reverse TCP tunnel. Cancel the owning
Run when the smoke is complete:

```bash
axern run cancel <run-id>
```

The automated Compose checks remain the source of truth for exact setup and
acceptance behavior:

```bash
make local-compose-server-base-smoke
make local-compose-python-sdk-e2e
```

## Kind

Start kind, select its context, and repeat the same Run/Allocation commands:

```bash
make kind-up
axern ctx use kind
axern ctx list
```

The product contract is identical in both environments. Only the local endpoint
and generated context credentials differ.

## Observability

Compose and kind start the local OpenTelemetry stack by default. To disable it
for a leaner run:

```bash
OTEL=0 make local-compose-up
OTEL=0 make kind-up
```

For Compose, Grafana LGTM listens on `http://127.0.0.1:13000`; for kind it
listens on `http://127.0.0.1:13001`. Filter by low-cardinality component data,
then use Run ID, Allocation ID, node ID, and tunnel session ID only in logs or
bounded diagnostic queries where supported.
