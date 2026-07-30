---
title: Python Service
description: Create an HTTP service with the Python SDK and reach it through the Axern gateway.
---

The maintained example creates an environment and a one-replica Service,
waits for a ready allocation, reaches its HTTP server through the public
`/svc` gateway route, and removes both resources.

![Creating, reaching, and cleaning up an image-backed Python Service through Axern](/terminal/python-service.gif)

```bash
uv run --package axern-sdk \
  python sdk/python/examples/service_gateway.py
```

The example reads the endpoint and mTLS identity from the selected Axern
context. It starts from a portable Python OCI image, uses `runc` for the trusted
long-lived workload, serves `Hello from Axern`, verifies the gateway response,
waits for Service deletion, and performs defensive cleanup. Read the complete
source here:
[`sdk/python/examples/service_gateway.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/service_gateway.py).

While a Service is active, the CLI reads the same authoritative resource:

```bash
axern service get <service-id> --output json
```

Service state remains authoritative in `controld`; the SDK does not select a
node or cache a route. Gateway resolution and endpoint health stay platform
concerns.
