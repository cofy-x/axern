# Axern Go Libraries

`lib/go` contains repository-internal Go libraries shared by multiple Axern
services and tools.

Current packages:

- [`agentbundle`](./agentbundle): shared agent image mount layout and binary-path validation.
- [`grpcclient`](./grpcclient): small gRPC dialing and readiness helpers.
- [`imageref`](./imageref): shared container image reference parsing and local
  insecure registry matching helpers.
- [`nodecapability`](./nodecapability): canonical observed node-capability
  catalog, extension validation, and snapshot eligibility rules shared by the
  node runtime and control plane.
- [`observability`](./observability): shared OpenTelemetry setup, metrics helpers, and logrus hook.

External Go SDK code belongs in [`../../sdk/go`](../../sdk/go).
