# Gateway Python Smoke

This smoke verifies an Axern gateway. The implementation uses the public Python
SDK, but the purpose is deployment verification: connect to the gateway control
edge, create a sandbox, create a service, reach the service through the gateway
HTTP edge, and then clean up the created resources.

## Configure

Copy the template to an ignored local file and fill values from an installed CLI
context in `~/.config/axern/config.json`:

```bash
cp examples/smoke/gateway-python/axern.env.example work/axern-gateway-smoke.env
$EDITOR work/axern-gateway-smoke.env
```

The smoke disables local proxy environment variables inside the Python process
before opening control-plane and HTTP connections. This matches deployments
where the gateway is reached directly from an allowlisted client IP.

Run from the repository root:

```bash
set -a
source work/axern-gateway-smoke.env
set +a
uv run --package axern-sdk python examples/smoke/gateway-python/smoke.py
```

The script prints the catalog template count, sandbox allocation, gateway HTTP
status, and cleanup status.
