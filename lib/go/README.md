# Axern Go Libraries

`lib/go` contains repository-internal Go libraries shared by multiple Axern services and tools.

Current packages:

- [`grpcclient`](./grpcclient): gRPC dialing/readiness and internal workload TLS transport primitives.
- [`imageref`](./imageref): shared container image reference parsing and local insecure registry matching helpers.
- [`networkpolicy`](./networkpolicy): canonical sandbox egress-policy validation, normalization, and enforcement-mode classification shared by the control plane and node runtime.
- [`nodecapability`](./nodecapability): canonical observed node-capability definition registry, extension validation, and snapshot eligibility rules shared by the node runtime and control plane.
- [`observability`](./observability): shared OpenTelemetry setup, metrics helpers, and logrus hook.

External Go SDK code belongs in [`../../sdk/go`](../../sdk/go).
