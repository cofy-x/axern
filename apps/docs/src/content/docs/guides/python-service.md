---
title: Python Service
description: Create an HTTP service with the Python SDK and reach it through the Axern gateway.
---

The maintained example creates an Environment and a one-replica Service,
waits for a ready allocation, reaches its HTTP server through the public
`/svc` gateway route, and removes both resources. It requires `uv` and a
working Axern context with an HTTP `service_url`; the Compose and Kubernetes
quickstarts create that context field for you.

![Creating, reaching, and cleaning up an image-backed Python Service through Axern](/terminal/python-service.gif)

```bash
# From the repository root; this uses the checked-out SDK package.
uv run --package axern-sdk \
  python sdk/python/examples/service_gateway.py
```

For an installed SDK, use `uv add axern-sdk==<version>` and adapt the same
`AxernClient.create_environment()` and `create_service()` calls to your
application.

The example reads the endpoint and mTLS identity from the selected Axern
context. It starts from a portable Python OCI image, uses `runc` for the trusted
long-lived workload, serves `Hello from Axern`, verifies the gateway response,
waits for Service deletion, and performs defensive cleanup. Read the complete
source here:
[`sdk/python/examples/service_gateway.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/service_gateway.py).

The gateway URL is assembled as:

```text
<context.service_url>/svc/<namespace>/<service-id>/<container-port>/
```

The example listens on `0.0.0.0:8080`, so its route is
`/svc/default/<service-id>/8080/` when using the default namespace. If the
selected context has no `service_url`, set `AXERN_SERVICE_URL` to the gateway
HTTP base URL before running the example.

While a Service is active, the CLI reads the same authoritative resource:

```bash
axern service get <service-id> --output json
axern service replicas <service-id>
axern service events <service-id>
```

Service state remains authoritative in `controld`; the SDK does not select a
node or cache a route. Gateway resolution and endpoint health stay platform
concerns. If the example fails before cleanup, use the printed resource IDs to
delete the Service and Environment manually after checking their state.
