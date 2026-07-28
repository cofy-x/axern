# bpfnet Production Regression Runbook

This runbook is the repeatable Kubernetes validation flow for bpfnet production
replacement. It avoids cluster names, regions, registry names, image tags,
kubeconfigs, and rollout revisions. Use it with
[Production Replacement Baseline](production-replacement-baseline.md).

## Preconditions

- The target cluster runs real Linux Kubernetes nodes.
- `node-all-in-one` is deployed with `node.network.natBackend=ebpf`.
- The benchmark image is the `axnoded-verify` image for the same source build,
  not the production `node-all-in-one` image.
- The benchmark namespace can pull the image through an existing pull secret.
- Local access to the kube-apiserver must use the intended direct network path.
  Clear proxy environment variables when the local proxy would intercept that
  path.

Set the common environment:

```bash
export KUBECONFIG=/path/to/kubeconfig
export KUBE_NAMESPACE=axern-system
export BENCHMARK_IMAGE=registry.example.com/axnoded-verify:tag
export BENCHMARK_IMAGE_PULL_SECRETS=acr-pull
export OUTPUT_ROOT="${PWD}/work/axnoded-bpfnet-benchmark"
```

All examples below intentionally clear proxy variables:

```bash
export KUBE_ENV='env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy -u ALL_PROXY -u all_proxy'
```

## Readiness Gate

Before benchmarking, every node pod must report a ready TC dataplane:

```bash
$KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" get pods \
  -l app.kubernetes.io/component=node

for pod in $($KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" get pods \
  -l app.kubernetes.io/component=node -o name); do
  pod="${pod#pod/}"
  $KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" exec "${pod}" -c node -- \
    bpfnetctl check --json
  $KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" exec "${pod}" -c node -- \
    bpfnetctl status --json
done
```

Required result:

- `bpfnetctl check --json` returns `.ok=true`.
- `status.state.fullFallback=false`.
- `status.attachment.ingressTcAttached=true`.
- `status.attachment.egressTcAttached=true`.
- `status.attachment.pinnedMapsReady=true`.
- `status.attachment.pinnedProgramsReady=true`.

`localhost-tcp-iptables-compat` is acceptable when the kernel does not support
the localhost cgroup path. `iptables-full-fallback` is a rollback state and
fails production replacement validation.

## Ingress Comparison

```bash
out="${OUTPUT_ROOT}/ingress"
rm -rf "${out}" && mkdir -p "${out}"

$KUBE_ENV \
  KUBECONFIG="${KUBECONFIG}" \
  KUBE_NAMESPACE="${KUBE_NAMESPACE}" \
  BENCHMARK_IMAGE="${BENCHMARK_IMAGE}" \
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS}" \
  BENCHMARK_RUNS=1 \
  BENCHMARK_REQUESTS=50000 \
  BENCHMARK_CONCURRENCY=128 \
  BENCHMARK_PATHS=external_tcp_ingress,external_udp_ingress \
  BENCHMARK_SNAT_POST_GC_WAIT=12s \
  OUTPUT_DIR="${out}" \
  runtime/axnoded/scripts/benchmark/benchmark-kubernetes-compare.sh
```

Summarize:

```bash
jq -r '
  .comparison[] |
  [.name,
   .iptables.throughputRps,
   .ebpf.throughputRps,
   ((.ebpf.throughputRps / .iptables.throughputRps - 1) * 100),
   .iptables.p95Ms,
   .ebpf.p95Ms,
   ((.ebpf.p95Ms / .iptables.p95Ms - 1) * 100),
   (.iptables.totalFailures // 0),
   (.ebpf.totalFailures // 0)] | @tsv
' "${out}/compare.json"
```

## Steady TCP And UDP Comparison

```bash
out="${OUTPUT_ROOT}/steady-udp"
rm -rf "${out}" && mkdir -p "${out}"

$KUBE_ENV \
  KUBECONFIG="${KUBECONFIG}" \
  KUBE_NAMESPACE="${KUBE_NAMESPACE}" \
  BENCHMARK_IMAGE="${BENCHMARK_IMAGE}" \
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS}" \
  BENCHMARK_RUNS=1 \
  BENCHMARK_REQUESTS=50000 \
  BENCHMARK_CONCURRENCY=128 \
  BENCHMARK_PATHS=egress_tcp_reuse,egress_tcp_pool,egress_udp,egress_udp_connected \
  BENCHMARK_SNAT_POST_GC_WAIT=12s \
  OUTPUT_DIR="${out}" \
  runtime/axnoded/scripts/benchmark/benchmark-kubernetes-compare.sh
```

Summarize:

```bash
jq -r '
  .comparison[] |
  [.name,
   .iptables.throughputRps,
   .ebpf.throughputRps,
   .iptables.p95Ms,
   .ebpf.p95Ms,
   (.iptables.totalFailures // 0),
   (.ebpf.totalFailures // 0),
   (.ebpf.snatMapPostGc.fwdEntries // 0),
   (.ebpf.snatMapPostGc.revEntries // 0),
   (.ebpf.snatMapPostGc.revAliasEntries // 0),
   (.ebpf.kernelDelta.snatAllocExhausted // 0),
   (.ebpf.kernelDelta.snatTcpReverseMissSynAcks // 0),
   (.ebpf.kernelDelta.snatTcpNonSynMissFwdHostMismatches // 0)] | @tsv
' "${out}/compare.json"
```

## TCP Short-Connection Churn

Use the multi-client path to stress bpfnet shared maps without stopping at one
client namespace's ephemeral-port boundary.

```bash
out="${OUTPUT_ROOT}/tcp-short"
rm -rf "${out}" && mkdir -p "${out}"

$KUBE_ENV \
  KUBECONFIG="${KUBECONFIG}" \
  KUBE_NAMESPACE="${KUBE_NAMESPACE}" \
  BENCHMARK_IMAGE="${BENCHMARK_IMAGE}" \
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS}" \
  BENCHMARK_RUNS=1 \
  BENCHMARK_REQUESTS=90000 \
  BENCHMARK_CONCURRENCY=64 \
  BENCHMARK_MULTI_CLIENT_COUNT=2 \
  BENCHMARK_PATHS=egress_tcp_short_multi_client \
  BENCHMARK_SNAT_POST_GC_WAIT=12s \
  OUTPUT_DIR="${out}" \
  runtime/axnoded/scripts/benchmark/benchmark-kubernetes-compare.sh
```

Required result:

- eBPF failures are `0`.
- eBPF stays production-comparable to iptables for throughput and P95.
- `snatAllocExhausted=0`.
- `snatTcpReverseMissSynAcks=0`.
- `snatTcpNonSynMissFwdHostMismatches=0`.
- post-GC forward, reverse, and alias entries are `0`.

Late `snatTcpNonSynMisses` and `snatFallbackHits` can be healthy close-path
tail traffic. Treat them as a bug only when they correlate with failures,
reverse SYN-ACK misses, host mismatches, allocator exhaustion, or persistent
post-GC retention.

## eBPF-Only Soak

```bash
out="${OUTPUT_ROOT}/ebpf-soak"
rm -rf "${out}" && mkdir -p "${out}"

$KUBE_ENV \
  KUBECONFIG="${KUBECONFIG}" \
  KUBE_NAMESPACE="${KUBE_NAMESPACE}" \
  BENCHMARK_IMAGE="${BENCHMARK_IMAGE}" \
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS}" \
  BENCHMARK_BACKENDS=ebpf \
  BENCHMARK_RUNS=6 \
  BENCHMARK_REQUESTS=90000 \
  BENCHMARK_CONCURRENCY=64 \
  BENCHMARK_MULTI_CLIENT_COUNT=2 \
  BENCHMARK_PATHS=egress_tcp_short_multi_client,egress_tcp_reuse,egress_tcp_pool \
  BENCHMARK_SNAT_POST_GC_WAIT=12s \
  OUTPUT_DIR="${out}" \
  runtime/axnoded/scripts/benchmark/benchmark-kubernetes-compare.sh
```

Summarize all run JSON files:

```bash
SOAK_REPORT_DIR="${out}/ebpf" python3 - <<'PY'
import json, statistics
import os
from pathlib import Path

base = Path(os.environ["SOAK_REPORT_DIR"])
rows = {}
for report in sorted(base.glob("run-*.json")):
    data = json.loads(report.read_text())
    for path in data["paths"]:
        name = path["name"]
        summary = path["summary"]
        kernel = path.get("kernelDelta") or {}
        post_gc = path.get("snatMapPostGc") or {}
        row = rows.setdefault(name, {
            "runs": 0, "requests": 0, "failures": 0,
            "rps": [], "p95": [], "post_fwd": [], "post_rev": [],
            "post_alias": [], "alloc": 0, "synack": 0, "host_mismatch": 0,
        })
        row["runs"] += 1
        row["requests"] += summary["requests"]
        row["failures"] += summary["failures"]
        row["rps"].append(summary["throughputRps"])
        row["p95"].append(summary["latency"]["p95Ms"])
        row["post_fwd"].append(post_gc.get("fwdEntries", 0))
        row["post_rev"].append(post_gc.get("revEntries", 0))
        row["post_alias"].append(post_gc.get("revAliasEntries", 0))
        row["alloc"] += kernel.get("snatAllocExhausted", 0)
        row["synack"] += kernel.get("snatTcpReverseMissSynAcks", 0)
        row["host_mismatch"] += kernel.get("snatTcpNonSynMissFwdHostMismatches", 0)

for name, row in rows.items():
    print(name, row["runs"], row["requests"], row["failures"],
          f"avg_rps={statistics.mean(row['rps']):.3f}",
          f"avg_p95_ms={statistics.mean(row['p95']):.3f}",
          f"max_p95_ms={max(row['p95']):.3f}",
          f"max_post_fwd={max(row['post_fwd'])}",
          f"max_post_rev={max(row['post_rev'])}",
          f"max_post_alias={max(row['post_alias'])}",
          f"alloc={row['alloc']}",
          f"synack={row['synack']}",
          f"host_mismatch={row['host_mismatch']}")
PY
```

The soak passes when every path has zero failures, the risk counters stay zero,
and post-GC maps drain to zero in every run.

## Rollout Recovery

Run this after benchmark coverage or after changing the deployed bpfnet image:

```bash
$KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" rollout restart ds/node-all-in-one
$KUBE_ENV kubectl -n "${KUBE_NAMESPACE}" rollout status ds/node-all-in-one --timeout=10m
```

Then repeat the readiness gate and a small eBPF-only smoke:

```bash
out="${OUTPUT_ROOT}/post-rollout-smoke"
rm -rf "${out}" && mkdir -p "${out}"

$KUBE_ENV \
  KUBECONFIG="${KUBECONFIG}" \
  KUBE_NAMESPACE="${KUBE_NAMESPACE}" \
  BENCHMARK_IMAGE="${BENCHMARK_IMAGE}" \
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS}" \
  BENCHMARK_BACKENDS=ebpf \
  BENCHMARK_RUNS=1 \
  BENCHMARK_REQUESTS=10000 \
  BENCHMARK_CONCURRENCY=64 \
  BENCHMARK_PATHS=egress_tcp_reuse,egress_tcp_pool,egress_udp \
  BENCHMARK_SNAT_POST_GC_WAIT=12s \
  OUTPUT_DIR="${out}" \
  runtime/axnoded/scripts/benchmark/benchmark-kubernetes-compare.sh
```

## Pass Criteria

The production regression passes when:

- all benchmark eBPF paths have zero failures;
- bpfnet mode is not `iptables-full-fallback`;
- TC ingress and egress remain attached on every node;
- pinned maps and pinned programs are ready on every node;
- allocator exhaustion, reverse SYN-ACK misses, and host mismatches stay zero;
- post-GC SNAT forward, reverse, and alias maps drain to zero for egress paths;
- rollout restart returns every node pod to ready and post-rollout smoke passes.
