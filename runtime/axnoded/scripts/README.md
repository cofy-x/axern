# Scripts

This directory contains the Docker, Kubernetes benchmark, demo, and tooling
wrappers used by `axnoded` development and verification.

This document is about **script semantics and knobs**, not the full validation
matrix. For the authoritative verification matrix, use
[Verification](../docs/verification.md).

## Recommended Entry Points

Core Docker truth-path:

```bash
make verify-sandboxd-e2e
make verify-sandboxd-provider-e2e
make verify-sandboxd-oci-e2e
make verify-docker
make verify-docker-runsc-ebpf
make verify-bpfnetctl-e2e
make verify-docker-runsc-debug
make verify-docker-runc-debug
```

Fast sandboxd contract loop:

```bash
make verify-sandboxd-provider-smoke
```

Benchmarks and focused profiles:

```bash
make benchmark-startup-matrix
make benchmark-docker-runsc-compare
BENCHMARK_IMAGE=registry.example.com/axnoded-verify:tag make benchmark-kubernetes-runsc-compare
make profile-docker-runsc-egress-udp-iptables
make profile-docker-runsc-egress-udp-ebpf
make profile-docker-runsc-external-tcp-iptables
make profile-docker-runsc-external-tcp-ebpf
```

Local demo and tooling:

```bash
make run-dashboard-nginx-demo
make example-bpfnet-udp-ingress
make example-bpfnet-egress
make verify-docker-build
make protos-docker
```

## Script Layout

- `lib/`
  - shared helpers for Docker, verify, benchmark, profile, and demo flows
- `verify/`
  - host-side wrappers and in-container runners for truth-path verification,
    including focused sandboxd provider conformance checks
- `benchmark/`
  - Docker benchmark wrappers and focused `perf` entrypoints
- `demo/`
  - supported local dashboard demo workflow
- `cache/`
  - reusable host-side caches for heavy external artifacts
- `tools/`
  - helper entrypoints such as `protos-docker.sh`

## Environment Variables

Common build and verify knobs:

```bash
IMAGE_TAG=axnoded-verify:latest
CONTAINER_NAME=axnoded-verify
VERIFY_DOCKER_PLATFORM=linux/arm64
VERIFY_DOCKER_BUILDKIT=1
VERIFY_DOCKER_PULL=false
VERIFY_DOCKER_HTTP_PROXY=http://host.docker.internal:7890
VERIFY_DOCKER_HTTPS_PROXY=http://host.docker.internal:7890
VERIFY_DOCKER_NO_PROXY=localhost,127.0.0.1,host.docker.internal
OCI_TEST_IMAGE_SOURCE=auto
APT_MIRROR_SOURCE=archive
CARGO_REGISTRY_SOURCE=crates-io
GOPROXY=https://proxy.golang.org,direct
GOSUMDB=sum.golang.org
BASE_IMAGE=ubuntu:24.04
RUNTIME_UNDER_TEST=runsc
RUNTIME_BINARY=/usr/local/bin/runsc
NAT_BACKEND=iptables
```

OCI-backed functional E2E tests accept `OCI_TEST_IMAGE_SOURCE=auto`,
`docker-cache`, or `registry`. The default `auto` mode publishes a cached host
image to the repo-managed local registry and otherwise lets `imagemgr` fetch
the original ref. Use `docker-cache` for deterministic regional regression
after pre-pulling the image, and use `registry` when the configured remote
registry path itself is under test. `OCI_TEST_LOCAL_REGISTRY_PORT` overrides
the repo-managed registry port used by the cache-backed path.

Benchmark and profile knobs:

```bash
BENCHMARK_REQUESTS=1000
BENCHMARK_CONCURRENCY=16
BENCHMARK_WARMUP_REQUESTS=64
BENCHMARK_MULTI_CLIENT_COUNT=4
BENCHMARK_SNAT_POST_GC_WAIT=12s
BENCHMARK_RUNS=3
BENCHMARK_RUN_RETRIES=3
BENCHMARK_PATHS=egress_udp,egress_udp_connected
BENCHMARK_PROFILE_MODE=stat
BENCHMARK_PROFILE_EVENTS=task-clock,context-switches,cpu-migrations,page-faults
BENCHMARK_PROFILE_RETRIES=3
STARTUP_MATRIX_SCENARIOS=runsc-local,runc-local,runsc-oci
BENCHMARK_IMAGE=registry.example.com/axnoded-verify:tag
KUBE_NAMESPACE=axern-system
BENCHMARK_IMAGE_PULL_SECRETS=registry-pull
BENCHMARK_NODE_NAME=
BPFNET_SNAT_GC_INTERVAL=1s
BPFNET_SNAT_TCP_CLOSING_TIMEOUT=2s
BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT=10s
```

TCP egress benchmarks are split by connection lifecycle. Use
`egress_tcp_short` for one TCP connection per request, `egress_tcp_reuse` for
one reused connection per worker, and `egress_tcp_pool` for a small reused
connection pool per worker. Prefer running these paths explicitly when
investigating TCP short-connection failures instead of mixing them into a broad
ingress/UDP benchmark. For eBPF short-connection churn, compare
`snatMapAfter` with `snatMapPostGc` and `snatMapGcReleased`; the post-GC
snapshot waits `BENCHMARK_SNAT_POST_GC_WAIT` after the phase ends and requires a
single egress transport per `verify-egress` run.

For unconnected UDP churn, inspect `snatMapAfter.revUdpEntries`,
`snatMapPostGc.revUdpEntries`, `snatMapAfter.udpTranslatedPortsUsed`, and
`snatMapPostGc.udpTranslatedPortsUsed` in addition to the total map entry
counts. Retained UDP entries should fall after the configured datagram idle
timeout plus one GC interval; increase
`BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT` for long-idle UDP or QUIC-like traffic.

Use `egress_tcp_short_multi_client` when the single-client `egress_tcp_short`
run reaches the client namespace ephemeral-port boundary before bpfnet itself is
under pressure. This path starts `BENCHMARK_MULTI_CLIENT_COUNT` suite sandboxes
inside one axnoded instance, splits total requests/concurrency across them, and
reports one combined path. The default multi-client count is `4`.

For bpfnet TCP short-connection churn, prefer the coherent `snatMapPeak`
snapshot over only looking at end-of-phase `snatMapAfter`. A healthy
full-close release path should keep `snatMapPeak.translatedPortsUsed` close to
the active concurrency level, with `snatAllocExhausted=0` and
`snatFullCloseMarks` increasing as map entries enter full-closing state.
`snatTcpFullCloseDeletes` should rise when terminal ACK-like packets release
full-closing mappings. Later `snatTcpNonSynMissFwdLookups` usually means extra
local FIN/RST/ACK packets arrived after a mapping was already released; treat
that as expected close churn when failures remain zero and post-GC maps are
empty. `snatTcpReverseMissSynAcks` is higher risk because it means an inbound
SYN-ACK missed a reverse mapping for a tuple bpfnet had previously managed.
If peak translated-port usage approaches the `SNAT_PORT_MIN..SNAT_PORT_MAX`
pool while active entries remain low, the dataplane is retaining closed
mappings too long and will regress against iptables under tcp-short churn.
Use
[bpfnet Production Replacement Baseline](../../../network/bpfnet/docs/production-replacement-baseline.md)
as the reusable production replacement comparison point.

The startup matrix default is intentionally limited to stable Docker regression
scenarios: `runsc-local`, `runc-local`, and `runsc-oci`. Run
`STARTUP_MATRIX_SCENARIOS=runsc-nydus make benchmark-startup-matrix` when
validating Nydus image startup as a focused image-runtime path.

Demo and workflow-specific knobs:

```bash
PROTO_IMAGE_TAG=axnoded-proto-tools:latest
HOST_PORT=18080
READY_TIMEOUT=60
KEEP_RUNNING=true
DASHBOARD_HOST_PORT=23001
DEMO_CONTAINER_NAME=axnoded-dashboard-nginx-demo
RUNSC_HOST_PORT=18080
RUNC_HOST_PORT=18081
AXNODED_IDLE_RUNTIME_RETENTION_TTL=5m
AXNODED_IDLE_RUNTIME_RETENTION_MAX=128
```

## Behavior Notes

- `run-dashboard-nginx-demo.sh` is the supported local demo surface. It starts
  the dashboard container, prints the dashboard URL, and expects the managed
  `runsc` / `runc` nginx sandboxes to be started or stopped from `/demo/nginx`.
- The dashboard demo is local-rootfs-only. `imagemgr` and `imagefsd` remain
  `disabled` in `/inventoryz`.
- With `NAT_BACKEND=ebpf`, the dashboard remains available and `/demo/nginx`
  still works, but managed nginx host URLs are best-effort only.
- In an eBPF demo or verify container, use `bpfnetctl check` for a read-only
  readiness check of pinned maps, pinned programs, links, and tc attachment.
- `make verify-bpfnetctl-e2e` starts an eBPF dashboard demo, validates
  `bpfnetctl check --json` before and after creating the managed `runsc` and
  `runc` nginx instances, and explicitly gates pinned program readiness.
- For supported verify targets and their semantic intent, use
  [Verification](../docs/verification.md), not this file.
- Kubernetes benchmark runs use a temporary privileged Job per backend/run and
  require the axnoded verify image. They are intended for real Linux node
  dataplane validation before switching a deployed `node-all-in-one` DaemonSet
  to `NAT_BACKEND=ebpf`.

## Direct Script Usage

If you intentionally bypass `make`, the common direct entrypoints are:

```bash
bash scripts/verify/verify-docker.sh
bash scripts/verify/verify-docker-debug.sh
bash scripts/benchmark/startup-matrix-docker.sh
bash scripts/tools/protos-docker.sh
```

In normal usage, prefer `make` so the repository keeps one stable entrypoint
surface.

## TCP Short-Connection Churn Notes

`verify-egress` records client-side resource snapshots for each benchmark path
from inside the suite sandbox that opens the benchmark connections. The report
includes that sandbox's `RLIMIT_NOFILE`, Linux ephemeral port range, TCP
TIME_WAIT sysctls, `/proc/net/sockstat` when available, `/proc/net/tcp{,6}`
fallback counters, end-of-phase deltas, and sampled peak deltas while the phase
is running. Use `clientPeak`, `clientPeakDelta`, `clientAfter`, and
`clientDelta` in `report.json` or aggregated `compare.json` when investigating
tcp-short failures. In runsc, `/proc/net/tcp{,6}` peak counters are usually more
useful than end-of-phase counters because short connections can disappear
before the phase ends. If iptables and eBPF both fail with `connect: resource
temporarily unavailable` while `clientPeakDelta.tcpTableEstablished`,
`clientPeak.tcpTable.entries`, or `clientPeakDelta.tcpTimeWait` grows against
the same ephemeral port range, treat the result as a client churn boundary
before optimizing bpfnet map lookup or SNAT allocation code. Switch to
`egress_tcp_short_multi_client` to continue stressing the shared bpfnet maps
with multiple source namespaces after that boundary is confirmed.
