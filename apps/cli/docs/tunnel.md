# Axern Tunnel

`axern tunnel open` creates a foreground reverse TCP tunnel from one running allocation to a local TCP service on the machine running `axern`.

Start the local target, create a detached Run, and obtain its allocation ID:

```bash
python3 -m http.server 8080 --bind 127.0.0.1
axern run --detach python:3.12-slim -- python -c 'import time; time.sleep(3600)'
axern run get <run-id>
```

Open the tunnel:

```bash
axern tunnel open \
  --allocation-id <allocation-id> \
  --local 127.0.0.1:8080
```

The command prints the allocation-local bind address. A process inside the allocation can connect to that loopback address and reach the local target. Keep the command running while the workload needs the target; Ctrl-C revokes the session.

Inspect or diagnose sessions without exposing their tokens:

```bash
axern tunnel list --allocation-id <allocation-id>
axern tunnel inspect <session-id>
axern tunnel events <session-id>
axern tunnel doctor --session-id <session-id> --local 127.0.0.1:8080
axern tunnel doctor --allocation-id <allocation-id> --local 127.0.0.1:8080
```

Tunnel relay connections use the gateway control edge mTLS path. Repo-managed Compose and kind contexts configure the same public entry model used in production.

Explicit cleanup is available when the foreground connector is no longer running:

```bash
axern tunnel revoke <session-id> --reason manual-cleanup
axern run cancel <run-id>
```
