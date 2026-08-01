---
title: Reverse Tunnels
description: Expose a local TCP service to code running inside an Axern service or sandbox.
---

An Axern tunnel is a reverse TCP tunnel: code inside a remote allocation calls
a localhost port that Axern binds inside the allocation, and that traffic is
forwarded back to a TCP target on your workstation. Use it when remote
workloads need a development API server, a mock, or a local credential-holding
proxy. It is the opposite of a port-forward; your machine does not use the
tunnel to call the remote service.

:::caution[Local network exposure]

Every process in the remote allocation can reach the tunnel's local target.
Do not point a tunnel at a production database, cloud metadata endpoint, or
an administrative interface. Bind local development servers to loopback,
use a short-lived session, and treat the remote workload as trusted for the
duration of the tunnel. The tunnel does not make the local target public to
the internet, but it does cross the sandbox boundary by design.

:::

## Tunnel from a Service

Start the local target first, then open a foreground tunnel from a ready
service replica:

```bash
python3 -m http.server 8080 --bind 127.0.0.1

axern service create --template-id python311 --replicas 1
axern service tunnel <service-id> --to 127.0.0.1:8080
```

The command selects a stable ready replica, creates a tunnel session, waits
for the allocation-local bind, and prints the session and bind addresses:

```text
Service: svc-...
Selected allocation: alloc-...
Tunnel session: tun-...
Local target: 127.0.0.1:8080
Remote bind: 127.0.0.1:42377
Press Ctrl-C to revoke the tunnel.
```

Inside the allocation, `curl http://127.0.0.1:42377/` now reaches your local
`127.0.0.1:8080`. Target a specific replica with `--allocation-id` or
`--node-id`. Keep the command running while the remote workload needs the
local target; Ctrl-C revokes the session.

`axern tunnel open --allocation-id <allocation-id> --local 127.0.0.1:8080` is
the lower-level allocation-scoped entrypoint for debugging.

## Tunnel from an SDK sandbox

The Python SDK owns the connector and renews the tunnel TTL while the sandbox
is active:

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context("~/.config/axern/config.json")

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    upstream="127.0.0.1:8080",
    remote_port=8786,
) as sandbox:
    print(sandbox.bound_addr)
```

## Diagnose and clean up

```bash
axern tunnel list --allocation-id <allocation-id>
axern tunnel inspect <session-id>
axern tunnel doctor --service-id <service-id> --local 127.0.0.1:8080
axern tunnel revoke <session-id> --reason manual-cleanup
```

Doctor checks control-plane state, gateway relay reachability, recent peer
events, and the local upstream probe. Its JSON output intentionally excludes
tunnel tokens. Relay connections use the gateway control edge mTLS path, so
development and production contexts use the same public entry model.

For the full session lifecycle and relay path, see the repository's
[tunnel document](https://github.com/cofy-x/axern/blob/main/apps/cli/docs/tunnel.md).
