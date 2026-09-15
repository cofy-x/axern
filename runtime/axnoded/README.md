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

- `axern.node.sandbox.v1.NodeSandbox`: gateway-forwarded process, terminal, file/archive, readiness, and Computer Use operations for one explicit Allocation. The routable node listener accepts this service only from the verified `gatewayd` mTLS identity. Public messages never carry access credentials; axnoded accepts an AllocationAccessGrant only from private incoming gRPC metadata and rejects missing or ambiguous values. Streaming operations acknowledge a validated grant before consuming request data or producing sandbox output.
- `axern.private.node.lifecycle.v1.NodeLifecycle`: repo-internal control-plane-to-node allocation create, delete, and status, accepted on the routable listener only from the verified `controld` mTLS identity.
- `axern.private.node.operator.v1.NodeOperator`: root-only local operator inspection, Allocation-scoped exec/wait, and audited break-glass workflows for `axctl`.
- `axern.private.node.network.v1.AllocationNetwork`: narrow machine-only Allocation network resolution for `node-tunneld`, registered on a separate Unix socket.
- `axern.private.control.node.v1.NodeControl`: ordered atomic node reports with explicit finite execution-lease grants, coalesced Allocation lifecycle batches, and allocation-access-grant replication with `controld`.

The reporter uses an explicitly admitted Node identity and a fresh process identity for observation ordering. An administrator admits the Node ID with a one-time enrollment token. The node generates its private key locally, registers its CSR on the dedicated enrollment listener, and automatically renews its 24-hour certificate. Normal reports and watches authenticate the exact Node URI, never the enrollment token. Revocation and retirement irreversibly reject reports, renewal, status batches, and watches; only retirement confirms that control-plane cleanup obligations are clear. Replacement requires a new Node ID.

Node lifecycle requests carry resolved secret env vars, resolved secret files, request-scoped registry auth, ports, network mode, egress policy, and read-only image mounts as typed fields. `axnoded` validates that contract before computing its request digest or creating side effects, materializes inputs into the Allocation-local runtime environment, and cleans up Allocation-scoped files on teardown. It does not pack execution behavior into JSON or OCI labels. Writable rootfs, retained stdout/stderr, and workspace data are Allocation-local; callers must transfer required bytes before teardown.

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
  -network-socket /run/axnoded/network.sock \
  -http-address 127.0.0.1:23001
```

`make release-cli` is Linux-only. Build `axctl` inside the shared devbox or the verification container.

`make release-binary` also builds `output/axern-sandboxd`, the sandbox-local PID 1 supervisor. Packaged node images install it at `/usr/local/libexec/axnoded/`.

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
axctl allocation list
axctl image mounts
axctl allocation network-policy explain <allocation-id>
axctl allocation network-policy doctor --json <allocation-id>
axctl allocation exec <allocation-id> -- /bin/sh
axctl allocation force-terminate --reason 'incident 42' <allocation-id>
axctl allocation force-cleanup --reason 'incident 42' <allocation-id>

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

- root-only operator Unix socket: `/run/axnoded/axnoded.sock`
- machine-only Allocation network Unix socket: `/run/axnoded/network.sock`
- optional root-only local conformance Unix socket: disabled in production; verification images use `/run/axnoded/conformance.sock`
- repo-local dev socket: `.dev/run/axnoded.sock`
- repo-local machine socket: `.dev/run/axnoded-network.sock`
- HTTP operator surface: `127.0.0.1:23001`

The routable listener loads the node-owned `identity/node.pem` bundle under the configured root and `control_plane_tls_ca_cert`. Verified URI roles enforce the service matrix: gatewayd may call NodeSandbox and controld may call NodeLifecycle. Callers verify the exact Allocation-bound Node ID, not a shared DNS name.

The enrollment client authenticates controld before sending a one-time token from `-enrollment-token-file`. Published identity bypasses that file entirely; renewal runs with seven to eight hours remaining and bounded jittered backoff. Invalid or expired existing identity does not fall back to registration. Recovery and the ExecutionLease watchdog continue while enrollment or renewal is unavailable; no failed authentication extends execution authority.

Local lifecycle conformance is never registered on the operator socket or the routable listener. It is available only when `-conformance-socket` is explicitly configured, accepts only unbound local Allocations, and refuses to create, inspect, or delete any control-plane-bound Allocation.

`NodeOperator` remains root-only and Unix-socket-only. Its `Exec`, `ExecStream`, and `Wait` methods operate on the exact Allocation identity and cannot advance Allocation lifecycle. Destructive incident recovery is limited to reason-bearing `ForceTerminateAllocation` and `ForceCleanupAllocation`; normal cancellation and cleanup remain control-plane operations.

`AllocationNetwork` is registered on the separate machine socket and exposes only `ResolveAllocationNetwork` to node-local platform daemons such as `node-tunneld`. It cannot exec, inspect, terminate, or clean an Allocation.

Image-backed rootfs flows depend on the node-local `imagemgr` socket:

- default: `/var/run/imagemgr.sock`
- repo-local dev: `.dev/run/imagemgr.sock`

Cross-subsystem sockets and runtime relationships are tracked in [Runtime Stack](../../.x/runtime-stack.md). The storage ownership contract is summarized in [Storage Architecture](../../docs/architecture/storage-architecture.md).

Shared API contracts live in [SDK Proto Workspace](../../sdk/proto/README.md).

## Bounded Allocation Output

Normal control-plane cleanup carries the immutable output deadline. Axnoded seals stdout/stderr into an Allocation-keyed directory using synced files and an atomic manifest publication before deleting live logs. Reads hold the lifecycle lock through each bounded file read and use either live logs or the sealed snapshot. The manifest owns output deletion only, never execution identity or admission. Startup and a one-minute sweep remove expired snapshots; corrupt snapshots are unreadable and diagnosed without preventing recovery of unrelated running Allocations. Rootfs files are not retained. Node-disk loss and local break-glass cleanup do not guarantee output availability.
