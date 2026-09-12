# Examples

Examples live here when Axern exposes stable SDK or service interfaces.

Avoid adding placeholder demos that do not exercise real platform behavior.

## Smoke

- [`smoke/gateway-python`](smoke/gateway-python) verifies a deployed gateway with the Python SDK and direct gateway HTTP access.

## Load

- [`load/gateway-python`](load/gateway-python) runs guarded concurrent sandbox and service gateway load stages against a deployed gateway.
- [`load/startup-readiness-python`](load/startup-readiness-python) measures image-backed sandbox startup, service replica ready, and first gateway HTTP latency with externally built validation images.
