# Verification

This is the authoritative verification matrix for `runtime/axnoded`. Use
`README.md` for subsystem overview and `scripts/README.md` for script-level
knobs. Run commands from `runtime/axnoded` unless you intentionally use root
wrappers such as `make axnoded-verify-docker`.

Use the repository-wide [verification tiers](../../../docs/verification/local-full-verification.md):
targeted/host-safe checks are the normal development loop; affected Linux
integration is the local runtime gate; destructive real-OOM, disk-fill,
page-cache attribution, repeated concurrency matrices, and reserve calibration
are external environment qualification owned by the deployment workspace, not
this repository's local matrix. Do not place qualification workloads in a
periodic provider or per-allocation audit.

## Quick Check

```bash
make verify-docker-runsc-ebpf
```

For agent-facing documentation changes from the repository root, also run:

```bash
make agent-doc-check
```

`make verify-docker-conformance` is the required Linux truth gate for the
production memory boundary. It starts axnoded with cgroup enforcement enabled,
certifies both runc and runsc through the global serial lane, validates the
bounded `conformance` sibling, and then creates a normal workload without
resource-contention retries. The ordinary runtime profiles retain
`disabled_dev` to cover the explicit development contract; `make verify-docker`
runs both contracts.

## Sandboxd Layers

| Layer | Contract | Target |
| --- | --- | --- |
| daemon unit | Wire contract, provider registry, strict JSON, file/process/provider internals, diagnostics, errors | `go test ./internal/sandboxd/... ./internal/runtime/sandboxd/... ./cmd/verify-sandboxd-provider` |
| provider smoke | Fast direct daemon API contract, capability discovery, diagnostics, structured errors, small file/process loops | `make verify-sandboxd-provider-smoke` |
| provider full gate | Direct provider contract plus daemon, desktop provider, and OCI injection wrappers | `make verify-sandboxd-provider-e2e` |
| PID 1 daemon | Health/status API, user exit semantics, signal forwarding, fast-exit reaping | `make verify-sandboxd-e2e` |
| OCI injection | Sandboxd as PID 1 under `runc` and `runsc`, host bundle socket readiness, file/process/PTY/probe/ports/mounts | `make verify-sandboxd-oci-e2e` |
| optional desktop | Computer-use and browser provider hooks | `make verify-sandboxd-computer-use-e2e`, `make verify-sandboxd-desktop-e2e` |
| product broker | Public `NodeSandbox` file/process/desktop/browser behavior and local operator diagnostics through `axnoded` | `make verify-node-cli-e2e`, `make local-compose-refresh-verify`, `make local-compose-computer-use-e2e`, `go test ./internal/api ./axctl/commands/sandbox` |
| packaging | Release binaries and node image sandboxd libexec path | `make verify-sandboxd-packaging` |
| release readiness | Lightweight sandboxd pre-release gate across architecture, core tests, provider smoke, packaging, proto drift, and SDK capability checks | `make verify-sandboxd-release-readiness` |

## Sandboxd Coverage Matrix

| Runtime | Rootfs / Profile | Baseline Coverage | Optional Coverage | Gate |
| --- | --- | --- | --- | --- |
| direct daemon | host/container tmpfs | strict JSON, diagnostics, file/archive, process, PTY, probes, ports, mounts, structured errors | provider discovery hooks | `make verify-sandboxd-provider-e2e` |
| `runc` | sample OCI rootfs | PID 1 injection, bundle socket, lifecycle, file/process/PTY/probe diagnostics | none | `make verify-sandboxd-oci-e2e` |
| `runsc` | sample OCI rootfs | PID 1 injection, bundle socket, lifecycle, file/process/PTY/probe diagnostics | none | `make verify-sandboxd-oci-e2e` |
| `runsc` | Docker OCI image | node create/wait/kill, network, file/process/terminal through product APIs | browser/computer-use when image supports them | `make verify-docker-runsc-ebpf`, `make local-compose-refresh-verify` |
| `runc` | Docker OCI image | generic runtime confidence and sandboxd baseline behavior | image-dependent | `make verify-docker-runc` |
| `runsc` | OCI/Nydus/OSS rootfs | image manager integration, read-only mount handling, sandboxd runtime mount injection | image-dependent | `make verify-node-oci-e2e`, `make verify-node-nydus-e2e`, `make verify-node-oss-e2e` |
| `runsc` | `server-base` | SSH terminal semantics, sudo/nosuid expectations, probes, service smoke | none | `make local-compose-server-base-smoke` |
| `runsc` | `desktop-base` | normal sandbox lifecycle plus desktop session readiness | computer-use and browser | `make local-compose-computer-use-e2e` |

`make verify-sandboxd-provider-smoke` is the fast direct daemon/provider
contract gate. `make verify-sandboxd-provider-e2e` is the broad focused
sandboxd gate: it runs the direct provider contract, daemon E2E, optional
desktop provider E2E, and OCI injection E2E.
`make verify-sandboxd-packaging` is the fast release gate for sandboxd binary
presence and image path consistency.
`make verify-sandboxd-release-readiness` is the default pre-release gate for
sandboxd changes: run it before heavier Docker or compose validation when the
change touches daemon APIs, provider discovery, packaging, or SDK capability
models.

Recommended sandboxd release sequence:

```bash
make verify-sandboxd-release-readiness
make verify-sandboxd-provider-e2e
bash ../../scripts/verify-all.sh --from axnoded-verify-node-locality-e2e
```

Use `make verify-sandboxd-oci-e2e` directly when the change is narrowly scoped
to runtime bundle injection or PID 1 wiring.

Use the full root `verify-all` only when a release or broad runtime change needs
complete confidence. It is not the normal local edit loop and should not be
restarted from step one after a late failure. The root script prints the failed
step, raw command, and a copyable `--from <step>` resume command; reproduce the
exact failed step directly, fix it, then resume from that boundary.

## Release Assets

`make release-binary` builds `output/axnoded`,
`output/axnoded-runtime-runner`, and `output/axern-sandboxd`. Packaged node
images install `axern-sandboxd` at `/usr/local/libexec/axnoded/axern-sandboxd`.

Changing binary names, install paths, or node image packaging is a cross-runtime
packaging change. Update deployment values and runtime docs together, then run
`make verify-sandboxd-packaging`.

## Runtime Layers

| Layer | Contract | Target |
| --- | --- | --- |
| broad Docker runtime | Main `axnoded` runtime confidence | `make verify-docker` |
| runsc network/eBPF | runsc runtime plus eBPF networking path | `make verify-docker-runsc-ebpf` |
| debug runtime paths | Narrow runtime diagnostics | `make verify-docker-runsc-debug`, `make verify-docker-runc-debug` |
| bpfnet diagnostics | Pinned program readiness and managed runtime diagnostics | `make verify-bpfnetctl-e2e` |
| network-policy Linux correctness | Hermetic 32-cell runc/runsc × bridge/ebpf × IPv4/IPv6 × policy-mode truth with minimal, timing-independent samples | `make verify-network-policy-linux-matrix` |

## Node And Image Layers

| Layer | Target |
| --- | --- |
| CLI and allocation lifecycle | `make verify-node-cli-e2e` |
| inventory and startup observability | `make verify-node-inventory-e2e`, `make verify-node-startup-metrics-e2e`, `make verify-node-startup-matrix-smoke` |
| bundle and managed create/start gate | `make verify-node-bundle-template-e2e`, `make verify-node-cli-e2e` |
| service volumes/probes | `make verify-node-service-volumes-e2e`, `make verify-node-service-probes-e2e` |
| runtime profiles | `make verify-node-python-runtime-e2e`, `make build-python311-runtime-image` |
| retention/locality/warm pool | `make verify-node-retention-e2e`, `make verify-node-locality-e2e`, `make verify-node-warm-pool-e2e` |
| rootfs modes | `make verify-node-oci-e2e`, `make verify-node-nydus-e2e`, `make verify-node-oss-e2e` |

Node-local network-policy diagnostics are covered by
`go test ./internal/service ./internal/api ./axctl/commands/sandbox`. The gate
must exercise `absent`, `dns_deny`, `strict`, capability-unavailable,
enforcement-unhealthy, and stale-proof results, and must reject destination
names, Host/SNI, remote addresses, CIDR values, policy digests, and raw daemon
state from both the private operator response and stable JSON output.

`verify-node-locality-e2e` reports explicit phases for initial inventory, OCI
start, OCI retention, Nydus start, and Nydus heat. If create fails, inspect the
printed context in this order: image pull and imagemgr mount state, runtime
create output and sandbox stdout/stderr, sandboxd readiness in the runtime
stderr, `/readyz` and `/inventoryz`, then locality heat fields in the cached
inventory snapshot. The create timeout is script-configurable through
`CREATE_SANDBOX_TIMEOUT`; the default is `300s`, high enough for cold image plus
Nydus mount paths without relaxing the final locality assertions.

Startup observability uses two axnoded histogram levels. `axern.axnoded_startup_phase_duration_seconds`
keeps the stable operator view: language runtime lookup, rootfs preparation,
resource allocation, runtime bundle preparation, runtime launch, and network
activation. `axern.axnoded_startup_step_duration_seconds` is the attribution
view for rootfs and runtime internals such as rootfs cache lookup, rootfs wait,
rootfs mount, writable rootfs view preparation, bundle materialization, runtime
start, runtime state wait, and sandboxd readiness wait. Image-manager rootfs
internals are exported by imagemgr as `axern.imagemgr_timed_operation_stage_duration_seconds`;
use that metric to separate registry fetch, layer/bootstrap extraction, daemon
creation, overlay or loop mount, and mount-readiness cost.
For service startup, pair those node-side startup metrics with
`axern.controld_service_replica_stage_duration_seconds`,
`axern.controld_node_lifecycle_rpc_duration_seconds`,
`axern.axnoded_lifecycle_stage_duration_seconds`, and
`axern.controld_allocation_status_report_stage_duration_seconds`. Use
`axern.axnoded_allocation_status_queue_wait_duration_seconds` and
`axern.controld_service_status_batch_stage_duration_seconds` to separate
node-side coalescing delay from service lock, allocation update, and projection
cost. Together these metrics distinguish scheduler/placement admission,
controld-to-node lifecycle RPC, node lifecycle request handling, runtime
startup, readiness probe execution, queued status delivery, and durable status
projection.

Control-plane outage tests must confirm that `/control-planez` exposes a
bounded retry with retained terminal state, that new observations do not cause
an early RPC, and that recovery drains the queue and clears active failures.

Root-level equivalents exist for common node checks, for example:

```bash
make axnoded-verify-docker
make axnoded-verify-node-oci-e2e
make axnoded-verify-node-nydus-e2e
make axnoded-verify-node-oss-e2e
```

## Local Demos

```bash
make run-dashboard-nginx-demo
make example-bpfnet-udp-ingress
make example-bpfnet-egress
```

To exit the dashboard demo after startup:

```bash
KEEP_RUNNING=false NAT_BACKEND=iptables make run-dashboard-nginx-demo
KEEP_RUNNING=false NAT_BACKEND=ebpf make run-dashboard-nginx-demo
```

The dashboard demo uses local rootfs only. `imagemgr` and `imagefsd` should be
reported as `disabled` in `/inventoryz`.

## Performance

```bash
make benchmark-startup-matrix
make benchmark-docker-runsc-compare
BENCHMARK_IMAGE=registry.example.com/axnoded-verify:tag make benchmark-kubernetes-runsc-compare
```

`benchmark-startup-matrix` and `benchmark-docker-runsc-compare` are Docker
regression gates. They are useful before committing script or runtime changes,
but they are not production bpfnet replacement evidence. The startup matrix
defaults to `runsc-local`, `runc-local`, and `runsc-oci`; run
`STARTUP_MATRIX_SCENARIOS=runsc-nydus make benchmark-startup-matrix` for focused
Nydus startup validation.

Use `benchmark-kubernetes-runsc-compare` for bpfnet dataplane validation on a
real Linux Kubernetes node. The Job image must be the axnoded verify image, not
the production `node-all-in-one` image, because the benchmark needs the verify
binaries and sample rootfs. Clear local proxy variables when they would
intercept the Kubernetes API path, and point `BENCHMARK_IMAGE` at an image
available to the target namespace. For the production replacement command
matrix, expected fallback modes, and `snatMap*` diagnostics, use
[`network/bpfnet/docs/production-regression-runbook.md`](../../../network/bpfnet/docs/production-regression-runbook.md).
