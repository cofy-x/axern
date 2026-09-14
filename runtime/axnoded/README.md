# Axnoded

`axnoded` is Axern's node-local sandbox execution agent. It runs on a selected node and owns sandbox creation, OCI bundle generation, runtime lifecycle, resource coordination, node-local operator inspection, gateway-forwarded execution after `controld` placement, and request-scoped image-backed process.

`controld` is the product-facing control plane. `axnoded` is the node authority for executing an admitted allocation.

Rootfs sources are local directories or registry images (OCI/Nydus). Node locality reports use those same sources; object-store artifact storage does not provide workload rootfs mounts.

Axnoded owns the aggregate `runtime_slots` capacity contract reported to controld. It derives the aggregate from `max_instance_num`, active containers, and enabled resource-pool constraints. Disabled pools do not block inventory, but startup fails when a loaded runtime requires a disabled pool. Controld does not accept reports from older nodes that omit this contract, so such releases require a coordinated rebuild rather than mixed-version operation.

Axnoded also owns node capability observation. Providers publish complete typed facts through one snapshot manager; the shared capability contract derives workload-facing platform capabilities, while controld admits against the reported evidence and axnoded revalidates it for each allocation. Platform capabilities cannot be configured as strings or inferred from successful user sandboxes. See [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md).

Network-policy capability keys are owned by the network-health and derived providers. Until egressd is configured and its self-tests pass, the DNS-policy and strict-egress facts are explicitly unavailable, so controld cannot place a policy workload on that node.

Sandbox interface pools may be IPv4 or IPv6. Bpfnet's native packet programs remain IPv4-only; selecting `ebpf` with an IPv6 pool activates the explicit bridge/ip6tables compatibility path and publishes bridge, not bpfnet, capability evidence.

## Platform Role

`axnoded` serves these gRPC surfaces:

- `axern.node.sandbox.v1.NodeSandbox`: gateway-forwarded `exec`, `exec_stream`, `process`, `exec_image`, `process_image`, `wait`, archive transfer, and allocation HTTP proxy. Public messages never carry lease credentials; axnoded accepts the execution lease only from private incoming gRPC metadata and rejects missing or ambiguous values. Streaming operations acknowledge a validated execution lease before consuming request data or producing sandbox output.
- `axern.private.node.lifecycle.v1.NodeLifecycle`: repo-internal control-plane-to-node allocation create, delete, and status.
- `axern.private.node.operator.v1.NodeOperator`: local Unix-socket operator workflows for `axctl`.
- `axern.private.control.node.v1.NodeControl`: outbound registration, node reports, coalesced allocation lifecycle batches, and execution lease replication with `controld`.

The reporter uses a durable node identity. If an operator retires that identity, `controld` rejects registration, reports, status batches, and watches; the host must be removed and any replacement must use a new node ID. Retirement is not a temporary disconnect or a reporter recovery mechanism.

Node lifecycle requests carry resolved secret env vars, resolved secret files, request-scoped registry auth, ports, network mode, egress policy, and read-only image mounts as typed fields. `axnoded` validates that contract before computing its request digest or creating side effects, materializes inputs into the Allocation-local runtime environment, and cleans up Allocation-scoped files on teardown. It does not pack execution behavior into JSON or OCI labels. Writable rootfs and workspace data are Allocation-local; durable outputs require explicit output delivery.

## Architecture

Requests enter through `internal/api`, move through `internal/service`, then coordinate rootfs resolution, resources, persisted container state, the process-owned runsc executor, and sandbox-local `axern-sandboxd` operations. One axnoded process owns exactly one execution backend; Allocation requests and node-local metadata contain no runtime selector.

Firecracker is not a dormant plugin in this process. If it is qualified later, it will be a separate node implementation and node pool with its own guest image, networking, snapshot, agent, recovery, and capability evidence contracts. Placement selects a node whose isolation capabilities satisfy the immutable Allocation requirements; users do not select a backend name.

One process-owned BoltDB under the configured store directory persists node-local resource snapshots and one atomic runtime/image ownership record per allocation. The database is opened once during service construction and closed only after resource writers stop during shutdown.

Use [AGENTS.md](AGENTS.md) for package-boundary rules, and use [Runtime Stack](../../.x/runtime-stack.md) for cross-subsystem relationships.

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

`make release-cli` is Linux-only. Build `axctl` inside the shared devbox or the verification container.

`make release-binary` also builds `output/axnoded-runtime-runner`, the host lifecycle helper that persists the runsc runtime wait result. The build also includes `output/axern-sandboxd`, the sandbox-local PID 1 supervisor. Packaged node images install runtime helpers at `/usr/local/libexec/axnoded/`.

## Documentation

- [Configuration](docs/configuration.md): sample config meanings and common local/production profiles.
- [Architecture](docs/architecture.md): internal layers and primary request flows.
- [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md): cross-system observation, policy, admission evidence, and enforcement loss.
- [Resource Handling](docs/resource.md): Allocation resource bindings, accounting, recovery, and network backend invariants.
- [Sandbox Daemon](docs/sandbox-daemon.md): Axern sandbox daemon architecture for PID 1 supervision and daemon-backed sandbox operations.
- [Image Mounts](docs/image-mounts.md): read-only image mount primitive for composing task sandboxes with reusable bundles.
- [Sandboxd Capabilities](docs/sandboxd-capabilities.md): current sandboxd capability matrix, ownership rules, and provider semantics.
- [Verification](docs/verification.md): validation matrix and recommended gates.
- [Devbox Workflow](../../docs/operations/devbox.md): repository-root Linux workspace for daily development.
- [Runtime Logs](../../docs/operations/runtime-logs.md): cross-component runtime log meanings.

## Verification

Use [Verification](docs/verification.md) as the authoritative validation matrix. Common entrypoints:

```bash
make test-host
make check-architecture
make verify-docker-runsc-ebpf
make verify-docker
```

For sandboxd-specific work, start with `make verify-sandboxd-release-readiness` or `make verify-sandboxd-oci-e2e` depending on the change. For local demo or script knobs, use [Runtime Scripts](scripts/README.md).

## Operations

Runtime image contracts live in [Runtime Images](docker/runtimes/README.md). Docker, Kubernetes benchmark, demo, and tooling wrappers live in [Runtime Scripts](scripts/README.md).

`axctl` is the node-local operator CLI shipped with `axnoded`; use `axern` for product workflows. Common local inspection starts with:

```bash
axctl node check
axctl sandbox list
axctl image mounts
axctl sandbox network-policy explain <sandbox-id>
axctl sandbox network-policy doctor --json <sandbox-id>

# Qualification-only: evict one page-aligned regular-file range from an exact,
# currently mounted image identity. This never uses the global drop_caches knob.
axctl image drop-page-cache \
  --ref registry.example/fixture@sha256:<digest> \
  --path /qualification/lower-payload.bin \
  --offset 0 \
  --length 33554432
```

Network-policy diagnostics are read-only and intentionally bounded. They show the effective mode, stable health category, Allocation ID, live enforcement revision, exact-binding state, and normalized rule counts. They do not return DNS names, HTTP Host, TLS SNI, destination IP/CIDR values, policy digests, or raw egressd records. `doctor` exits non-zero for unavailable capability, unhealthy enforcement, or a binding mismatch; a sandbox with no policy is reported as `absent` and is not considered degraded.

The daemon HTTP surface is operator-only: readiness and liveness, cached inventory at `/inventoryz`, control-plane reporter health at `/control-planez`, and local metrics at `/debug/metricsz`. It does not expose workload creation or interactive application endpoints. Production metrics use the shared OTEL pipeline.

Allocation lifecycle delivery uses a bounded, Allocation-keyed queue. The first terminal observation is persisted in a narrow outbox before delivery and survives runtime cleanup and process restart until controld acknowledges the exact observation. Failed batches retry with jittered exponential backoff from 100 milliseconds to 5 seconds; newer non-terminal observations may coalesce, while immutable terminal evidence is never replaced.

`/control-planez` reports queue, in-flight, retry, and recent result state. The same backlog and retry signals are exported through OTEL.

Axnoded OTEL metrics include the stable `axern.node_id` datapoint attribute so multiple node processes cannot overwrite the same cumulative time series. Cluster queries should aggregate across that label; node diagnostics may group by it directly.

`/debug/metricsz` is a versioned, bounded, process-local JSON snapshot used by startup and bpfnet verification to compare measurements before and after a test. It is not a Prometheus endpoint or a production scrape target.

## Interfaces And Sockets

Default local endpoints:

- axnoded Unix socket: `/run/axnoded/axnoded.sock`
- repo-local dev socket: `.dev/run/axnoded.sock`
- HTTP operator surface: `127.0.0.1:23001`

When `-grpc-address` is configured, `axnoded` can expose a routable TCP listener for `NodeSandbox` and `NodeLifecycle`.

`NodeOperator` remains Unix-socket-only. It also exposes `ResolveSandboxNetwork` for node-local platform daemons such as `node-tunneld`.

Image-backed rootfs flows depend on the node-local `imagemgr` socket:

- default: `/var/run/imagemgr.sock`
- repo-local dev: `.dev/run/imagemgr.sock`

Cross-subsystem sockets and runtime relationships are tracked in [Runtime Stack](../../.x/runtime-stack.md). The storage ownership contract is summarized in [Storage Architecture](../../docs/architecture/storage-architecture.md).

Shared API contracts live in [SDK Proto Workspace](../../sdk/proto/README.md).
