# Gateway Python Load

This example runs a guarded gateway load test with the public Python SDK. It is
intended for production-like validation after a deployment is healthy, not for
routine smoke checks.

The load test has two phases:

- `sandbox`: concurrently creates service-backed sandboxes, runs a command,
  writes and reads a file, and closes each sandbox.
- `service`: creates one service per stage with `N` replicas, waits for all
  replicas to become ready, runs the configured HTTP profiles through the
  gateway service edge, and cleans up the service and environment.

The HTTP profiles are:

- `keepalive`: multiple requests per persistent client connection.
- `short`: one TCP connection per request.
- `large`: a validated 4 MiB response.
- `stream`: a validated 1 MiB chunked response with separate first-byte latency.
- `lb`: replica identity coverage; the stage fails unless every ready replica serves traffic.

The default `python311` template supports `keepalive` and `short`. The other profiles use the `tiny-go-http` validation image built from Forge. Configure `AXERN_LOAD_IMAGE_REF`, its registry credential ID, and `AXERN_LOAD_HTTP_PROFILES=keepalive,short,large,stream,lb`; image source and template source are mutually exclusive.

## Configure

Copy the template to an ignored local file and fill values from an installed CLI
context:

```bash
cp examples/load/gateway-python/axern.env.example work/axern-gateway-load.env
$EDITOR work/axern-gateway-load.env
```

For a six-node ACK cluster with roughly `1930m` CPU and `2.32Gi` memory
allocatable per node, start with the default stages:

```bash
AXERN_LOAD_STAGES=6,12,24,36
AXERN_LOAD_REQUEST_CPU=100m
AXERN_LOAD_REQUEST_MEMORY=128Mi
```

The script removes local proxy environment variables before opening gRPC and
HTTP connections. This matches deployments where gateway access is direct from
an allowlisted client IP.

Set `AXERN_LOAD_PROMETHEUS_URL` to the deployment Prometheus API to capture
reset-aware gateway and axnoded HTTP proxy stage deltas. The benchmark waits
for both unified OTel histograms to contain every measured and warmup request;
an incomplete metrics snapshot fails the stage.

Run from the repository root:

```bash
set -a
source work/axern-gateway-load.env
set +a
uv run --package axern-sdk python examples/load/gateway-python/load.py
```

The output is JSON lines with per-stage summaries and individual failures. A
non-zero exit means at least one stage failed. Created services and environments
are cleaned up unless the process is interrupted hard.
