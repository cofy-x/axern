# Axnoded

`axnoded` is Axern's node-local sandbox execution agent. It runs on a selected
node and owns sandbox creation, OCI bundle generation, runtime lifecycle,
resource coordination, node-local operator inspection, gateway-forwarded
execution after `controld` placement, and request-scoped image-backed process.

`controld` is the product-facing control plane. `axnoded` is the node authority
for executing an admitted allocation.

Axnoded owns the aggregate `runtime_slots` capacity contract reported to
controld. It derives the aggregate from `max_instance_num`, active containers,
and enabled resource-pool constraints. Disabled pools do not block inventory,
but startup fails when a loaded runtime requires a disabled pool. Controld does
not accept reports from older nodes that omit this contract, so such releases
require a coordinated rebuild rather than mixed-version operation.

Axnoded also owns node capability observation. Providers publish complete typed
facts through one snapshot manager; the shared catalog derives workload-facing
platform capabilities, while controld admits against the reported evidence and
axnoded revalidates it for each allocation. Platform capabilities cannot be
configured as strings or inferred from successful user sandboxes. See
[Observed Capability Providers](../../docs/architecture/observed-capability-providers.md).

## Platform Role

`axnoded` serves these gRPC surfaces:

- `axern.node.sandbox.v1.NodeSandbox`: gateway-forwarded `exec`, `exec_stream`,
  `process`, `exec_image`, `process_image`, `wait`, archive transfer, and
  allocation HTTP proxy. Streaming operations acknowledge a validated execution
  lease before consuming request data or producing sandbox output.
- `axern.private.node.lifecycle.v1.NodeLifecycle`: repo-internal
  control-plane-to-node allocation create, delete, and status.
- `axern.private.node.operator.v1.NodeOperator`: local Unix-socket operator
  workflows for `axctl`.
- `axern.control.node.v1.NodeControl`: outbound registration, node reports,
  coalesced allocation status batches, and execution lease replication with
  `controld`.

The reporter uses a durable node identity. If an operator retires that identity,
`controld` rejects registration, reports, status batches, and watches; the host
must be removed and any replacement must use a new node ID. Retirement is not a
temporary disconnect or a reporter recovery mechanism.

Node lifecycle requests may include resolved secret env vars, resolved secret
files, request-scoped registry auth, service probes, and resolved node volume
specs. `axnoded` materializes those inputs into the allocation-local runtime
environment, asks `volumed` to publish resolved storage volumes, and cleans up
allocation-scoped files on teardown. Published volume results and release
observations are returned in node lifecycle responses so `controld` can update
`storaged`; `axnoded` does not call `storaged` directly.

## Architecture

Requests enter through `internal/api`, move through `internal/service`, then
coordinate rootfs resolution, volumes, resources, persisted container state, OCI
runtime handlers, and sandbox-local `axern-sandboxd` operations.

One process-owned BoltDB under the configured store directory persists node-local
resource snapshots and one atomic runtime/image ownership record per allocation.
The database is opened once during service construction and closed only after
resource writers stop during shutdown.

Use [AGENTS.md](AGENTS.md) for package-boundary rules, and use
[Runtime Stack](../../.x/runtime-stack.md) for cross-subsystem relationships.

## Build And Run

From `runtime/axnoded`:

```bash
# daemon + operator CLI
make

# daemon only
make release-binary

# host-safe tests
make test-host
```

Example daemon invocation:

```bash
./output/axnoded \
  -config ./docs/sample_conf.toml \
  -socket /run/axnoded/axnoded.sock \
  -http-address 127.0.0.1:23001
```

`make release-cli` is Linux-only. Build `axctl` inside the shared devbox or the
verification container.

`make release-binary` also builds `output/axnoded-runtime-runner`, the one-shot
helper used by OCI runtime handlers to run `runc`/`runsc` and persist exit
state, and `output/axern-sandboxd`, the sandbox-local PID 1 supervisor.
Packaged node images install runtime helpers at `/usr/local/libexec/axnoded/`.

## Documentation

- [Configuration](docs/configuration.md): sample config meanings and
  common local/production profiles.
- [Architecture](docs/architecture.md): internal layers and primary request
  flows.
- [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md):
  cross-system observation, policy, admission evidence, and enforcement loss.
- [Resource Handling](docs/resource.md): resource claims, pools, accounting, and
  network backend invariants.
- [Sandbox Daemon](docs/sandbox-daemon.md): Axern sandbox daemon
  architecture for PID 1 supervision and daemon-backed sandbox operations.
- [Image-Backed Process](docs/image-backed-process.md): `ExecImage` /
  `ProcessImage` execution model, mount rules, and lifecycle contract.
- [Image Mounts](docs/image-mounts.md): read-only image mount primitive for
  composing task sandboxes with reusable bundles.
- [Workspace Images](docs/workspace-images.md): TaskSet payload variant,
  copy-on-write workspace, and protected asset phase contract.
- [Sandboxd Capabilities](docs/sandboxd-capabilities.md): current
  sandboxd capability matrix, ownership rules, and provider semantics.
- [Verification](docs/verification.md): validation matrix and recommended gates.
- [Devbox Workflow](../../docs/operations/devbox.md): repository-root Linux
  workspace for daily development.
- [Runtime Logs](../../docs/operations/runtime-logs.md): cross-component
  runtime log meanings.

## Verification

Use [Verification](docs/verification.md) as the authoritative validation
matrix. Common entrypoints:

```bash
make test-host
make check-architecture
make verify-docker-runsc-ebpf
make verify-docker
```

For sandboxd-specific work, start with `make verify-sandboxd-release-readiness`
or `make verify-sandboxd-oci-e2e` depending on the change. For local demo or
script knobs, use [Runtime Scripts](scripts/README.md).

## Operations

Runtime image contracts live in [Runtime Images](docker/runtimes/README.md).
Docker, Kubernetes benchmark, demo, and tooling wrappers live in
[Runtime Scripts](scripts/README.md).

`axctl` is the node-local operator CLI shipped with `axnoded`; use `axern` for
product workflows. Common local inspection starts with:

```bash
axctl node check
axctl sandbox list
axctl image mounts
```

The daemon HTTP surface exposes a read-only dashboard, cached inventory at
`/inventoryz`, control-plane reporter health at `/control-planez`, local metrics
at `/debug/metricsz`, pprof, and the nginx demo. Production metrics use the
shared OTEL pipeline.

Allocation status delivery uses a bounded, allocation-keyed in-process queue.
Failed batches preserve terminal observations and retry with jittered
exponential backoff from 100 milliseconds to 5 seconds. New observations
coalesce without bypassing an active retry. The queue is not durable;
`controld` and node inventory remain responsible for convergence.

`/control-planez` reports queue, in-flight, retry, and recent result state. The
same backlog and retry signals are exported through OTEL.

Axnoded OTEL metrics include the stable `axern.node_id` datapoint attribute so
multiple node processes cannot overwrite the same cumulative time series.
Cluster queries should aggregate across that label; node diagnostics may group
by it directly.

`/debug/metricsz` is a versioned, bounded, process-local JSON snapshot used by
startup and bpfnet verification to compare measurements before and after a
test. It is not a Prometheus endpoint or a production scrape target.

## Interfaces And Sockets

Default local endpoints:

- axnoded Unix socket: `/run/axnoded/axnoded.sock`
- repo-local dev socket: `.dev/run/axnoded.sock`
- HTTP operator surface: `127.0.0.1:23001`

When `-grpc-address` is configured, `axnoded` can expose a routable TCP listener
for `NodeSandbox` and `NodeLifecycle`.

`NodeOperator` remains Unix-socket-only. It also exposes
`ResolveSandboxNetwork` for node-local platform daemons such as `node-tunneld`.

Image-backed rootfs flows depend on the node-local `imagemgr` socket:

- default: `/var/run/imagemgr.sock`
- repo-local dev: `.dev/run/imagemgr.sock`

Resolved node volume flows depend on the node-local `volumed` socket:

- default: `/run/volumed/volumed.sock`
- repo-local dev: `.dev/run/volumed.sock`

Cross-subsystem sockets and runtime relationships are tracked in
[Runtime Stack](../../.x/runtime-stack.md).
The storage ownership contract is summarized in
[Storage Architecture](../../docs/architecture/storage-architecture.md).

Shared API contracts live in [SDK Proto Workspace](../../sdk/proto/README.md).
